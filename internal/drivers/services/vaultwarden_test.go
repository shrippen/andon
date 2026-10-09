package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestVaultwardenKeepsSession: Vaultwarden allows three admin logins in
// five minutes (ADMIN_RATELIMIT_*), and Andon reads it for the tile, the
// connection test and the link status. One login serves until its
// cookie expires (401); a refused login says so instead of "login failed".
func TestVaultwardenKeepsSession(t *testing.T) {
	var logins atomic.Int32
	session := "s1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin":
			if logins.Add(1) > 2 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: vaultwardenCookie, Value: session})
			w.WriteHeader(http.StatusSeeOther)
		case "/admin/users":
			if c, err := r.Cookie(vaultwardenCookie); err != nil || c.Value != session {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`[{"email":"a@b.c"}]`))
		}
	}))
	defer srv.Close()
	api := VaultwardenApi{URL: srv.URL, Token: "admin-token"}

	for range 3 {
		if _, err := api.Users(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Fatalf("%d logins for three reads, want one", logins.Load())
	}

	// The cookie expired: one new login.
	session = "s2"
	if _, err := api.Users(context.Background()); err != nil || logins.Load() != 2 {
		t.Fatalf("after expiry: %v, %d logins", err, logins.Load())
	}

	// Vaultwarden refuses further logins.
	session = "s3"
	if _, err := api.Users(context.Background()); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("refused login: %v", err)
	}
}
