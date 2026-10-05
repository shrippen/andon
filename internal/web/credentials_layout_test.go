package web_test

import (
	"net/http"
	"net/http/httptest"
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

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "space_id": {instanceSpace(t, srv, client)}, "service": {"kimai"},
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

// instanceSpace is the id of the instance space, where templates live (a
// personal space holds only fixed connections).
func instanceSpace(t *testing.T, srv *httptest.Server, client *http.Client) string {
	t.Helper()
	form := string(mustGet(t, srv, client, "/connections/new?service=kimai"))
	m := regexp.MustCompile(`<option value="(\d+)">Instanz</option>`).FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("no instance space in:\n%s", form)
	}
	return m[1]
}
