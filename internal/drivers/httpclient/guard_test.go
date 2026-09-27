package httpclient_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"andon/internal/drivers/httpclient"
)

// TestGuardChecksRedirectTargets: an allowed host must not bounce a
// request to a denied address, e.g.
//
//	http://localhost:A  --302-->  http://127.0.0.1:B  (denied)
func TestGuardChecksRedirectTargets(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"secret": true}`))
	}))
	defer target.Close()
	bounce := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer bounce.Close()

	httpclient.SetGuard(func(host string, addrs []net.IP) bool { return host == "localhost" })
	defer httpclient.SetGuard(nil)

	start := strings.Replace(bounce.URL, "127.0.0.1", "localhost", 1)
	_, _, err := httpclient.GetJSON(context.Background(), start, httpclient.Options{})
	var denied httpclient.EgressDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected EgressDenied for the redirect target, got %v", err)
	}
}

// TestRequestsReuseConnections: two calls to one host share a connection
// instead of a new TCP/TLS handshake (and a leaked idle one) each time.
func TestRequestsReuseConnections(t *testing.T) {
	var opened atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			opened.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	for range 3 {
		if _, _, err := httpclient.GetJSON(context.Background(), srv.URL, httpclient.Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if n := opened.Load(); n != 1 {
		t.Fatalf("expected 1 connection, got %d", n)
	}
}

// TestSetGuardWhileRequesting: the admin may change the network policy
// while fetches run (go test -race).
func TestSetGuardWhileRequesting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	defer httpclient.SetGuard(nil)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				_, _, _ = httpclient.GetJSON(context.Background(), srv.URL, httpclient.Options{})
			}
		})
	}
	for range 20 {
		httpclient.SetGuard(func(string, []net.IP) bool { return true })
		httpclient.SetGuard(nil)
	}
	wg.Wait()
}
