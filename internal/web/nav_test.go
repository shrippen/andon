package web_test

import (
	"strings"
	"testing"
)

// TestNavCurrent: the menu marks the page you are on, including pages
// below a menu entry, and nothing else; the settings navigation marks
// exactly its page.
func TestNavCurrent(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	cases := []struct{ path, cur, other string }{
		{"/hints", `href="/hints" aria-current="page"`, `href="/billing" aria-current`},
		{"/hosts", `href="/hosts" aria-current="page"`, `href="/hints" aria-current`},
		{"/me/security", `<a href="/me/security" aria-current="page">`, `<a href="/me/notify" aria-current`},
	}
	for _, c := range cases {
		page := string(mustGet(t, srv, client, c.path))
		if !strings.Contains(page, c.cur) {
			t.Fatalf("%s: lacks %q", c.path, c.cur)
		}
		if strings.Contains(page, c.other) {
			t.Fatalf("%s: also marks %q", c.path, c.other)
		}
	}
}
