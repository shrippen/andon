package httpclient

import (
	"context"
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
