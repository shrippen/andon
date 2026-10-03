package web_test

import (
	"strings"
	"testing"
)

// TestAboutPage: the page shows the build, license and a menu entry.
func TestAboutPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/about"))
	for _, want := range []string{"Über Andon", "GPL-3.0", "Version", "Datenbank-Schema", "Gestartet", `href="/about"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("about lacks %q:\n%s", want, page)
		}
	}
}
