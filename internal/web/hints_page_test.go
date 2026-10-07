package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestHintsPageCompactAndBulk: lone hints are compact rows with "done"
// up front; the reason, the action link and the note wait behind "⋯".
// "All done" acknowledges every hint it names.
func TestHintsPageCompactAndBulk(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=invoiceninja"))[1]
	// Snipe-IT's demo brings rules with several hints: groups.
	for _, svc := range []string{"invoiceninja", "snipeit"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/hints"))
	row := regexp.MustCompile(`(?s)<li class="hint-card is-row".*?</li>`).FindString(page)
	more := regexp.MustCompile(`(?s)<details class="hint-more">.*?</details>`).FindString(row)
	for _, want := range []string{`class="btn btn-outline btn-sm hint-go"`, `class="hint-why"`, `name="note"`, `formaction="/hints/`} {
		if !strings.Contains(more, want) {
			t.Fatalf("row lacks %q behind ⋯:\n%s", want, row)
		}
	}
	if !strings.Contains(row, `<form method="post" action="/hints/`) {
		t.Fatalf("row without its done form:\n%s", row)
	}

	// Hints in a rule group are the same compact rows, under the group's
	// head and bulk bar.
	groups := regexp.MustCompile(`(?s)<section class="hint-rule".*?</section>`).FindAllString(page, -1)
	if len(groups) == 0 {
		t.Fatalf("no rule group:\n%s", page)
	}
	for _, g := range groups {
		if !strings.Contains(g, `class="hint-bulk"`) || strings.Contains(g, `<li class="hint-card" `) || !strings.Contains(g, `<li class="hint-card is-row"`) {
			t.Fatalf("group not as rows under its bulk bar:\n%s", g)
		}
	}

	ids := regexp.MustCompile(`<li class="hint-card[^"]*"[^>]* id="hint-(\d+)"`).FindAllStringSubmatch(page, -1)
	if len(ids) == 0 {
		t.Fatal("page lists no hints")
	}
	values := url.Values{"csrf": {csrf}, "action": {"ack"}}
	for _, id := range ids {
		values.Add("id", id[1])
	}
	if res := postForm(t, client, srv.URL+"/hints/bulk", values); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("bulk: %d", res.StatusCode)
	}
	after := string(mustGet(t, srv, client, "/hints"))
	for _, id := range ids {
		if strings.Contains(after, `id="hint-`+id[1]+`"`) {
			t.Fatalf("hint %s still open after bulk ack", id[1])
		}
	}
}

// TestHintsLayoutRemembered: "single" shows every hint as a compact row
// without group bars and stays chosen on the next visit; "grouped" brings
// the groups back. The filter side and its phone button come with both.
func TestHintsLayoutRemembered(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=invoiceninja"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"invoiceninja"}, "space_id": {string(space)},
		"name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/hints"))
	for _, want := range []string{`class="filter-layout"`, `class="filter-side"`, `class="btn btn-outline btn-sm filter-toggle"`, `aria-controls="hint-filter"`,
		`name="layout" value="grouped" aria-pressed="true"`, `class="filter-list"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("hints page lacks %q:\n%s", want, page)
		}
	}

	res := postForm(t, client, srv.URL+"/hints/layout", url.Values{"csrf": {csrf}, "layout": {"single"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("layout: %d", res.StatusCode)
	}
	page = string(mustGet(t, srv, client, "/hints"))
	if !strings.Contains(page, `name="layout" value="single" aria-pressed="true"`) || !strings.Contains(page, `class="hint-card is-row"`) {
		t.Fatalf("single layout not remembered:\n%s", page)
	}
	if strings.Contains(page, `class="hint-bulk"`) || strings.Contains(page, `class="hint-group-head"`) {
		t.Fatal("single layout still shows group bars")
	}

	postForm(t, client, srv.URL+"/hints/layout", url.Values{"csrf": {csrf}, "layout": {"grouped"}})
	if page = string(mustGet(t, srv, client, "/hints")); !strings.Contains(page, `name="layout" value="grouped" aria-pressed="true"`) {
		t.Fatal("grouped layout not restored")
	}
}

// TestNavDrawer: every page carries the phone menu: a button that
// controls a drawer with the boards, pages and account entries, the
// current page marked.
func TestNavDrawer(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/hints"))
	for _, want := range []string{`class="nav-burger" aria-controls="nav-drawer" aria-expanded="false"`, `<dialog class="nav-drawer" id="nav-drawer"`,
		`data-drawer-close`, `class="app-links nav-wide"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
	drawer := regexp.MustCompile(`(?s)<dialog class="nav-drawer".*?</dialog>`).FindString(page)
	for _, want := range []string{`href="/hints" aria-current="page"`, `href="/clients"`, `href="/billing"`, `href="/receipts"`, `href="/timeline"`, `href="/hosts"`, `action="/logout"`} {
		if !strings.Contains(drawer, want) {
			t.Fatalf("drawer lacks %q:\n%s", want, drawer)
		}
	}
}
