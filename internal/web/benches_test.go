package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// navBar is the bar's link strip of a page, without the menus.
func navBar(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `<nav class="app-links nav-wide"`)
	if start < 0 {
		t.Fatal("no bar")
	}
	return page[start : start+strings.Index(page[start:], "</nav>")]
}

// TestBenchFavs: without open hints and stars the bar shows no bench;
// a star puts it into the bar and onto the overview as pressed, a
// second one takes it out. The menu and the palette list every bench.
func TestBenchFavs(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	page := string(mustGet(t, srv, client, "/benches"))
	if strings.Contains(navBar(t, page), `href="/vat"`) {
		t.Fatal("bench in the bar without a star")
	}
	for _, want := range []string{`<summary>Werkbänke</summary>`, `class="launch" href="/vat"`, `action="/benches/vat/fav"`, `href="/benches" aria-current="page"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("overview lacks %q", want)
		}
	}

	res := postForm(t, client, srv.URL+"/benches/vat/fav", url.Values{"csrf": {csrf}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("star: %d", res.StatusCode)
	}
	page = string(mustGet(t, srv, client, "/benches"))
	if !strings.Contains(navBar(t, page), `href="/vat"`) || !strings.Contains(page, `aria-pressed="true">★`) {
		t.Fatal("star not kept")
	}

	postForm(t, client, srv.URL+"/benches/vat/fav", url.Values{"csrf": {csrf}})
	if page = string(mustGet(t, srv, client, "/benches")); strings.Contains(navBar(t, page), `href="/vat"`) {
		t.Fatal("star not removed")
	}

	if res := postForm(t, client, srv.URL+"/benches/nope/fav", url.Values{"csrf": {csrf}}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown bench: %d", res.StatusCode)
	}
	if palette := string(mustGet(t, srv, client, "/palette.json")); !strings.Contains(palette, `"kind":"bench","title":"Belege zuordnen","url":"/receipts"`) {
		t.Fatal("palette lacks the benches")
	}
}

// Every bench page keeps its header for the signed-in person, also
// after a failed write (?failed=1&who=…). The page values once named
// the failed entry "Who", the key the header reads the person from: the
// header showed no navigation, and with a name the page broke off.
func TestBenchPagesKeepHeader(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	for _, path := range []string{"/dunning", "/feeds", "/updates", "/subs", "/inventory", "/week", "/categorize",
		"/receipts", "/euer", "/purchases", "/vendors", "/vat", "/billing", "/clients", "/year", "/hosts", "/timeline"} {
		for _, query := range []string{"", "?failed=1&who=Acme+GmbH&reason=x"} {
			page := string(mustGet(t, srv, client, path+query))
			if !strings.Contains(page, `class="app-links`) || !strings.Contains(page, "</main>") {
				t.Errorf("%s%s: header or page missing", path, query)
			}
		}
	}
}
