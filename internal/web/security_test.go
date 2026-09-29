package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/settings"
)

// TestSecureHeadersOverHTTPS: behind TLS the browser is told to stay on
// HTTPS and to isolate the window from openers.
func TestSecureHeadersOverHTTPS(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	d := Deps{DB: database, Settings: settings.Settings{BaseURL: "https://andon.lan"}}
	rec := httptest.NewRecorder()
	d.Secure(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	h := rec.Header()
	if strings.Contains(h.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Errorf("CSP allows inline styles: %s", h.Get("Content-Security-Policy"))
	}
	if !strings.HasPrefix(h.Get("Strict-Transport-Security"), "max-age=") {
		t.Errorf("missing HSTS: %v", h)
	}
	if h.Get("Cross-Origin-Opener-Policy") != "same-origin" {
		t.Errorf("missing COOP: %v", h)
	}

	d.Settings.BaseURL = "http://andon.lan"
	rec = httptest.NewRecorder()
	d.Secure(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS over plain HTTP")
	}
}

// TestTokenURLsAreNotCached: a response to a ?token= URL (iframes and
// calendar clients cannot send headers) is neither cached nor passed on
// as referrer.
func TestTokenURLsAreNotCached(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	d := Deps{DB: database}
	rec := httptest.NewRecorder()
	d.Secure(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embed/hints?token=secret", nil))
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers: %v", rec.Header())
	}
}

// TestCrossSiteFormRefused: another site cannot post a login (login
// CSRF) or a reset; the browser marks such requests cross-site.
func TestCrossSiteFormRefused(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	d := Deps{DB: database}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for site, want := range map[string]int{"cross-site": http.StatusForbidden, "same-origin": http.StatusNoContent, "": http.StatusNoContent} {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("email=a&password=b"))
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		d.Secure(ok).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%q: %d, want %d", site, rec.Code, want)
		}
	}
}
