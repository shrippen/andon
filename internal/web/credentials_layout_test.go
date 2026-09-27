package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestCredentialsTableCells: the actions stay a table cell (a flex cell
// drew its row lines apart from the other columns) and every action is
// one kind of button, so rows keep one height.
func TestCredentialsTableCells(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)">`).FindStringSubmatch(string(mustGet(t, srv, client, "/boards/new")))[1]
	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "space_id": {space}, "service": {"kimai"},
		"name": {"Mine"}, "url": {"https://kimai.lan"}, "mode": {"personal"}, "tls": {"verify"}})
	conn := regexp.MustCompile(`/connections/(\d+)/edit`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	postForm(t, client, srv.URL+"/me/credentials/"+conn, url.Values{"csrf": {csrf}, "secret": {"tok"}})

	page := string(mustGet(t, srv, client, "/me/credentials"))
	if strings.Contains(page, `<td class="row-actions">`) {
		t.Fatal("actions cell is a flex container, not a table cell")
	}
	if !strings.Contains(page, `<div class="cred-actions">`) {
		t.Fatalf("actions not wrapped:\n%s", page)
	}
	actions := regexp.MustCompile(`(?s)<div class="cred-actions">(.*?)</td>`).FindStringSubmatch(page)[1]
	for _, kind := range []string{`class="link-btn"`, `summary class="link-btn`} {
		if strings.Contains(actions, kind) {
			t.Fatalf("mixed button kinds (%s):\n%s", kind, actions)
		}
	}
}
