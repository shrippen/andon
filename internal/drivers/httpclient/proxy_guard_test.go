package httpclient

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestProxyGuardChecksTarget: through an HTTP proxy the dial reaches
// only the proxy; the target must still pass the guard.
func TestProxyGuardChecksTarget(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("internal answer"))
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)

	SetGuard(func(host string, addrs []net.IP) bool { return host != "10.1.2.3" })
	defer SetGuard(nil)

	base := newTransport(TLSVerify)
	base.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: proxyGuard{base}}
	_, err := client.Get("http://10.1.2.3/secret")
	var denied EgressDenied
	if !errors.As(err, &denied) {
		t.Fatalf("proxied request to a denied host: %v", err)
	}
}
