package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestPlaceCreateForm: a new place starts without a kind (not "Zuhause"),
// and creating one on the demo says why nothing changed, translated.
func TestPlaceCreateForm(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := instanceSpace(t, srv, client)

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"dawarich"}, "space_id": {space},
		"name": {"Wege"}, "url": {"demo://dawarich"}, "mode": {"personal"}, "secret": {"demo"}, "tls": {"verify"}})
	conn := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]

	tab := string(mustGet(t, srv, client, "/connections/"+conn+"/places"))
	form := regexp.MustCompile(`(?s)action="/connections/\d+/places/create".*?</form>`).FindString(tab)
	if form == "" {
		t.Fatalf("no create form:\n%s", tab)
	}
	if !regexp.MustCompile(`name="kind"[^>]*><option value="">`).MatchString(form) {
		t.Fatalf("new place preset to a kind:\n%s", form)
	}

	resp = postForm(t, client, srv.URL+"/connections/"+conn+"/places/create", url.Values{"csrf": {csrf},
		"name": {"Bäcker"}, "lat": {"52.51"}, "lon": {"13.39"}, "kind": {""}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create answered %d", resp.StatusCode)
	}
	page := string(mustGet(t, srv, client, resp.Header.Get("Location")))
	if strings.Contains(page, "dns:") || !strings.Contains(page, "Demo-Verbindungen nehmen keine Änderungen an.") {
		t.Fatalf("create on the demo lacks a translated note:\n%s", page)
	}
}
