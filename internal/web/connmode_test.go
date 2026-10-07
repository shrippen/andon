package web_test

import (
	"regexp"
	"testing"
)

// A new connection in the personal space starts with a fixed login and
// shows its token field: there, "template" (everybody their own) means
// only oneself anyway.
func TestNewConnectionPersonalStartsFixed(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := mustGet(t, srv, client, "/connections/new?service=kimai")
	m := regexp.MustCompile(`id="space_id"[^>]*data-personal="(\d+)">\s*<option value="(\d+)"`).FindSubmatch(page)
	if m == nil || string(m[1]) != string(m[2]) {
		t.Fatalf("first space not marked personal: %q", m)
	}
	if !regexp.MustCompile(`<option value="shared"\s+selected>`).Match(page) {
		t.Fatal("personal space does not start with a fixed login")
	}
}
