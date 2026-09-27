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
	crypto.Init("test-master-key")
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	d := Deps{DB: database, Settings: settings.Settings{BaseURL: "https://andon.lan"}}
	rec := httptest.NewRecorder()
	d.Secure(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	h := rec.Header()
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
