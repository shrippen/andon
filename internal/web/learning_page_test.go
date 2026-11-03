package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The admin finds the learning page in the instance settings, saves its
// settings, trains the demo's Invoice Ninja and resets the model.
func TestLearningPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexpFirst(t, `<option value="(\d+)">`, mustGet(t, srv, client, "/connections/new?service=kimai")))
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})

	page := string(mustGet(t, srv, client, "/admin/learning"))
	if !strings.Contains(page, `href="/admin/learning" aria-current="page"`) || !strings.Contains(page, "Noch kein Modell") {
		t.Fatalf("page:\n%s", page)
	}

	resp := postForm(t, client, srv.URL+"/admin/learning", url.Values{"csrf": {csrfToken(t, srv, client)}, "mode": {"on"}, "min_percent": {"75"}, "hours": {"12"}})
	if resp.StatusCode >= 400 {
		t.Fatalf("save: %d", resp.StatusCode)
	}
	page = string(mustGet(t, srv, client, "/admin/learning"))
	if !strings.Contains(page, `name="min_percent" type="number" min="50" max="99" inputmode="numeric" value="75"`) {
		t.Fatalf("settings not kept:\n%s", page)
	}

	postForm(t, client, srv.URL+"/admin/learning"+"/train", url.Values{"csrf": {csrfToken(t, srv, client)}})
	page = string(mustGet(t, srv, client, "/admin/learning"))
	if !strings.Contains(page, "<td data-card=\"title\">Ninja</td><td>EÜR-Zeile</td>") {
		t.Fatalf("no model after training:\n%s", page)
	}
}

func regexpFirst(t *testing.T, pattern string, body []byte) []byte {
	t.Helper()
	m := regexp.MustCompile(pattern).FindSubmatch(body)
	if m == nil {
		t.Fatalf("no %s", pattern)
	}
	return m[1]
}
