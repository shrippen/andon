package web

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"andon/internal/settings"
)

// TestClientIPBehindProxy: behind a trusted proxy the client is the
// last address it appended; from anyone else the header is ignored
// (it could be forged to dodge the login limit).
func TestClientIPBehindProxy(t *testing.T) {
	d := Deps{Settings: settings.Settings{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}}}
	cases := []struct{ remote, forwarded, want string }{
		{"172.18.0.2:5000", "203.0.113.7", "203.0.113.7"},
		{"172.18.0.2:5000", "1.1.1.1, 203.0.113.7", "203.0.113.7"},
		{"172.18.0.2:5000", "203.0.113.7, 172.18.0.5", "203.0.113.7"},
		{"198.51.100.4:5000", "203.0.113.7", "198.51.100.4"},
		{"172.18.0.2:5000", "", "172.18.0.2"},
		{"[2001:db8::1]:443", "", "2001:db8::1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := d.clientIP(r); got != c.want {
			t.Errorf("%s via %q: %s, want %s", c.remote, c.forwarded, got, c.want)
		}
	}
}
