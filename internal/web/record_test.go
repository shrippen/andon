package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestConnectionRecord: a connection opens as a record with four tabs,
// the one the address names shown; its level is marked in the settings
// navigation; a fixed login is replaced on the access tab.
func TestConnectionRecord(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := instanceSpace(t, srv, client)

	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := postForm(t, &noFollow, srv.URL+"/connections", url.Values{"csrf": {csrf}, "space_id": {space}, "service": {"kimai"},
		"name": {"K"}, "url": {"http://127.0.0.1:1"}, "mode": {"shared"}, "tls": {"verify"}})
	id := regexp.MustCompile(`/connections/(\d+)\?welcome$`).FindStringSubmatch(resp.Header.Get("Location"))
	if id == nil {
		t.Fatalf("create leads to %q", resp.Header.Get("Location"))
	}
	record := "/connections/" + id[1]

	page := string(mustGet(t, srv, client, record+"?tab=access"))
	for _, want := range []string{
		`<a href="` + record + `?tab=access" aria-current="page">`,
		`id="tab-overview" hidden`, `id="tab-settings" hidden`, `id="tab-history" hidden`,
		`<a href="/spaces/` + space + `/connections" aria-current="page">`,
		`action="` + record + `/secret"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("record lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, `id="tab-access" hidden`) {
		t.Fatal("access tab hidden")
	}

	resp = postForm(t, &noFollow, srv.URL+record+"/secret", url.Values{"csrf": {csrf}, "secret": {"tok"}})
	if loc := resp.Header.Get("Location"); loc != record+"?tab=access&tested" {
		t.Fatalf("secret saved, back to %q", loc)
	}
	if page := string(mustGet(t, srv, client, record+"?tab=access")); !strings.Contains(page, "(unverändert lassen)") {
		t.Fatalf("login not stored:\n%s", page)
	}
}

// TestTileLinksToAccess: a tile on a template without the viewer's login
// links straight to the template's access tab.
func TestTileLinksToAccess(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := instanceSpace(t, srv, client)

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"kimai"}, "name": {"Zeit"}, "url": {"demo://kimai"}, "mode": {"personal"}, "tls": {"verify"}})
	conn := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space}, "type": {"table"},
		"title": {"Offen"}, "connection_id": {conn}, "cfg.table": {"unbilled_aging"}})
	boardURL, sectionID, version, widgetID := placeTarget(t, srv, client, "Offen")
	postForm(t, client, srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections/"+sectionID+"/place", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "widget_id": {widgetID}, "version": {version}})

	placement := string(placementRe.FindSubmatch(mustGet(t, srv, client, boardURL+"?edit"))[1])
	link := `href="/connections/` + conn + `?tab=access"`
	if frag := string(awaitFragment(t, srv, client, placement, link)); !strings.Contains(frag, link) {
		t.Fatalf("tile lacks %s:\n%s", link, frag)
	}
}
