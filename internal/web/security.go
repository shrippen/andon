package web

import (
	"net/http"
	"strings"

	"andon/internal/services/system"
)

const (
	embedPrefix = "/embed/"
	iconPrefix  = "/icons/"
	tokenParam  = "token"
)

// hsts keeps browsers on HTTPS for a year once they saw Andon over TLS.
const hsts = "max-age=31536000"

// csp builds the Content-Security-Policy: everything same-origin, iframe
// widgets only from admin-listed origins (and Andon's own pages, e.g. an
// attachment a dialog shows), and only /embed/ pages may be framed by
// other sites.
func (d Deps) csp(path string) string {
	frames := strings.TrimSpace("'self' " + strings.Join(system.IframeOrigins(d.DB), " "))
	ancestors := "'self'"
	if strings.HasPrefix(path, embedPrefix) {
		ancestors = "*"
	}
	// Markup carries no style attributes (andon.js applies data-style);
	// only icons keep inline styles, as uploaded SVGs use them.
	styles := "'self'"
	if strings.HasPrefix(path, iconPrefix) {
		styles += " 'unsafe-inline'"
	}
	// Dialog maps read tiles from the admin's source, glyphs and sprites
	// from Protomaps (kante-map.js).
	connect := "'self'"
	if origin := system.OriginOf(system.MapSource(d.DB)); origin != "" {
		connect += " " + origin + " " + mapAssets
	}
	return "default-src 'self'; script-src 'self'; style-src " + styles + "; " +
		"img-src 'self' data:; font-src 'self'; connect-src " + connect + "; object-src 'none'; " +
		"frame-src " + frames + "; frame-ancestors " + ancestors + "; base-uri 'self'; form-action 'self'"
}

// mapAssets serves the map's glyphs and sprites.
const mapAssets = "https://protomaps.github.io"

// crossOrigin refuses state-changing requests a browser marks as sent
// by another site (Sec-Fetch-Site, Origin): login CSRF and forged resets
// on routes without a session token. Requests without those headers
// (webhooks, API clients) pass.
var crossOrigin = http.NewCrossOriginProtection()

// Secure adds the security headers to every response, refuses
// cross-site form posts and frames error answers of pages (errorPages).
func (d Deps) Secure(next http.Handler) http.Handler {
	next = crossOrigin.Handler(d.errorPages(next))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", d.csp(r.URL.Path))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if d.Settings.SecureCookies() {
			h.Set("Strict-Transport-Security", hsts)
		}
		// A token in the URL (iframes, calendar clients) must not end up in
		// a cache or another site's referrer.
		if r.URL.Query().Has(tokenParam) {
			h.Set("Cache-Control", "no-store")
			h.Set("Referrer-Policy", "no-referrer")
		}
		next.ServeHTTP(w, r)
	})
}
