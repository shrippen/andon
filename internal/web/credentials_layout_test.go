package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTemplateActivation drives a template through the own connections
// page: activate it, see it active; after the admin moves it the page
// shows what changed and activates it again without a new token.
func TestTemplateActivation(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "space_id": {instanceSpace(t, srv, client)}, "service": {"kimai"},
		"name": {"Mine"}, "url": {"https://kimai.lan"}, "mode": {"personal"}, "tls": {"verify"}})
	conn := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	state := func() string {
		page := ownConnections(t, srv, client)
		m := regexp.MustCompile(`id="conn-` + conn + `" data-state="(\w+)"`).FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("template not on the page:\n%s", page)
		}
		return m[1]
	}
	if s := state(); s != "off" {
		t.Fatalf("new template: %s", s)
	}

	postForm(t, client, srv.URL+"/connections/"+conn+"/activate", url.Values{"csrf": {csrf}, "secret": {"tok"}})
	if s := state(); s != "active" {
		t.Fatalf("activated: %s", s)
	}

	postForm(t, client, srv.URL+"/connections/"+conn+"/edit", url.Values{"csrf": {csrf}, "name": {"Mine"}, "url": {"https://kimai.lan/v2"},
		"mode": {"personal"}, "tls": {"verify"}})
	if s := state(); s != "paused" {
		t.Fatalf("after the edit: %s", s)
	}
	page := ownConnections(t, srv, client)
	if !strings.Contains(page, `<td class="was">https://kimai.lan</td><td class="now">https://kimai.lan/v2</td>`) {
		t.Fatalf("no diff:\n%s", page)
	}

	postForm(t, client, srv.URL+"/connections/"+conn+"/activate", url.Values{"csrf": {csrf}})
	if s := state(); s != "active" {
		t.Fatalf("activated again: %s", s)
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

// ownConnections is the page /me/credentials leads to: the viewer's own
// connections and the templates to activate.
func ownConnections(t *testing.T, srv *httptest.Server, client *http.Client) string {
	t.Helper()
	resp := getFollowingRedirect(t, srv, client, "/me/credentials")
	defer resp.Body.Close()
	return readAll(t, resp)
}
