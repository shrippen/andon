package web_test

import (
	"net/url"
	"strings"
	"testing"
)

// The journal is reachable from the account menu and filters by area; a
// write shows there.
func TestJournalPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/journal"))
	for _, want := range []string{"Schreib-Journal", `href="/journal?area=vat"`, "Noch nichts geschrieben."} {
		if !strings.Contains(page, want) {
			t.Fatalf("journal lacks %q:\n%s", want, page)
		}
	}
	if !strings.Contains(string(mustGet(t, srv, client, "/hints")), `href="/journal"`) {
		t.Error("no journal link in the menu")
	}

	resp := postForm(t, client, srv.URL+"/journal/999/undo", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=journal.not_found") {
		t.Errorf("undo of nothing: %s", loc)
	}
}
