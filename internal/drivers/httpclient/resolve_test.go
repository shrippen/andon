package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDNS answers every host with 127.0.0.1 after delay and counts the
// lookups; the cache starts empty.
func fakeDNS(t *testing.T, delay time.Duration) *atomic.Int32 {
	var calls atomic.Int32
	old := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		calls.Add(1)
		time.Sleep(delay)
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	}
	resolved.Clear()
	SetGuard(func(string, []net.IP) bool { return true })
	t.Cleanup(func() {
		lookupIP = old
		resolved.Clear()
		SetGuard(nil)
	})
	return &calls
}

// closingServer forces a new connection, and so a dial, per request.
func closingServer(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
	}))
	t.Cleanup(srv.Close)
	return strings.Replace(srv.URL, "127.0.0.1", "svc.test", 1)
}

func get(t *testing.T, ctx context.Context, url string) {
	resp, err := Request(ctx, http.MethodGet, url, Options{})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// TestLookupCached: a burst of status checks asks DNS once per host, not
// twice per request (guard + dial) and again per redirect.
func TestLookupCached(t *testing.T) {
	calls := fakeDNS(t, 0)
	url := closingServer(t)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { get(t, context.Background(), url) })
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 lookup, got %d", n)
	}
}

// TestLookupTraced: the DNS wait shows in the request's trace, so a
// status check can leave it out of the service's response time.
func TestLookupTraced(t *testing.T) {
	const delay = 30 * time.Millisecond
	fakeDNS(t, delay)
	url := closingServer(t)

	var started time.Time
	var waited time.Duration
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { started = time.Now() },
		DNSDone:  func(httptrace.DNSDoneInfo) { waited += time.Since(started) },
	})
	get(t, ctx, url)

	if waited < delay {
		t.Fatalf("expected DNS wait ≥ %v in the trace, got %v", delay, waited)
	}
}

// TestLookupsBounded: 100 distinct hosts at once reach the resolver a few
// at a time. A home resolver (Pi-hole) drops queries from ~30 in flight
// on: 108 of 121 lookups timed out at 5 s when a board's links were
// checked all at once.
func TestLookupsBounded(t *testing.T) {
	const hosts = 100
	var inFlight, peak atomic.Int32
	old := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	}
	resolved.Clear()
	t.Cleanup(func() {
		lookupIP = old
		resolved.Clear()
	})

	var wg sync.WaitGroup
	for i := range hosts {
		wg.Go(func() {
			if _, err := resolve(context.Background(), fmt.Sprintf("h%d.test", i)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if p := peak.Load(); p > dnsParallel {
		t.Fatalf("%d lookups in flight, want ≤ %d", p, dnsParallel)
	}
}

// TestMissingHostCached: a name the resolver does not know is asked once
// per TTL, not on every status check.
func TestMissingHostCached(t *testing.T) {
	var calls atomic.Int32
	old := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		calls.Add(1)
		return nil, &net.DNSError{Err: "no such host", Name: "gone.lan", IsNotFound: true}
	}
	resolved.Clear()
	t.Cleanup(func() {
		lookupIP = old
		resolved.Clear()
	})

	for range 3 {
		if _, err := resolve(context.Background(), "gone.lan"); err == nil {
			t.Fatal("missing host resolved")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 lookup, got %d", n)
	}
}

// TestLookupWaitCancelled: a caller whose request ends stops waiting for
// a slow lookup; the lookup finishes for the cache.
func TestLookupWaitCancelled(t *testing.T) {
	release, finished := make(chan struct{}), make(chan struct{})
	old := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		defer close(finished)
		<-release
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	}
	resolved.Clear()
	t.Cleanup(func() {
		close(release)
		<-finished
		lookupIP = old
		resolved.Clear()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := resolve(ctx, "slow.test"); err == nil {
		t.Fatal("cancelled lookup resolved")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("caller waited %v", took)
	}
}
