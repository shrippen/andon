package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
)

// TestReceiptsPage: without connections the page says what is missing;
// with two Paperless it asks which to use, remembers the pick, and then
// suggests the demo scans for the demo expenses with their reasons.
func TestReceiptsPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/receipts"))
	if !strings.Contains(page, "Zwei Verbindungen nötig") || !strings.Contains(page, "/connections/new?service=paperless") {
		t.Fatalf("no hint about the missing connections:\n%s", page)
	}

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	conns := map[string]string{}
	for _, c := range []struct{ service, name string }{{"invoiceninja", "Ninja"}, {"paperless", "Archiv A"}, {"paperless", "Archiv B"}} {
		resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {c.service}, "name": {c.name}, "url": {"demo://" + c.service}, "mode": {"shared"}, "tls": {"verify"}})
		conns[c.name] = regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	}

	page = string(mustGet(t, srv, client, "/receipts"))
	if !strings.Contains(page, "Mit welchen Verbindungen arbeiten?") || !strings.Contains(page, "Archiv A") || !strings.Contains(page, "Archiv B") {
		t.Fatalf("no pick offered:\n%s", page)
	}
	if strings.Contains(page, `id="receipts-part"`) {
		t.Fatal("suggestions load before the pick")
	}

	resp := postForm(t, client, srv.URL+"/receipts/pick", url.Values{"csrf": {csrfToken(t, srv, client)}, "ninja": {conns["Ninja"]}, "paperless": {conns["Archiv B"]}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("pick: %d", resp.StatusCode)
	}
	page = string(mustGet(t, srv, client, "/receipts"))
	if strings.Contains(page, "Mit welchen Verbindungen arbeiten?") || !strings.Contains(page, `id="receipts-part"`) ||
		!regexp.MustCompile(`name="paperless" value="`+conns["Archiv B"]+`" checked`).MatchString(page) {
		t.Fatalf("pick not remembered or no switch bar:\n%s", page)
	}

	part := string(mustGet(t, srv, client, "/receipts/part?tab=match"))
	for _, want := range []string{"EX-0041", "Rechnung FE-2026-0917", "Verknüpfen", "„FE-2026-0917“ steht im Beleg (Titel)"} {
		if !strings.Contains(part, want) {
			t.Fatalf("suggestions lack %q:\n%s", want, part)
		}
	}
	if strings.Contains(part, "receipts.") || strings.Contains(part, "EX-0042") {
		t.Fatalf("raw key, or the linked expense suggested:\n%s", part)
	}

	// The 1∶n combo comes by itself, the sure match is offered in bulk,
	// the tab chips get their numbers.
	for _, want := range []string{"2 Belege", "Sichere Treffer", `value="demo1:201" checked`, `id="receipts-tab-match" href="/receipts?tab=match&amp;year=`, `hx-swap-oob="true"`} {
		if !strings.Contains(part, want) {
			t.Fatalf("suggestions lack %q:\n%s", want, part)
		}
	}
	queue := string(mustGet(t, srv, client, "/receipts/part?tab=queue"))
	if !strings.Contains(queue, "Tankquittung") || !strings.Contains(queue, "Zusammen mit weiteren Belegen") {
		t.Fatalf("queue lacks the tagged scan or the combo:\n%s", queue)
	}

	// A search shows the linked Kabelwerk scan as taken, without a link button.
	search := string(mustGet(t, srv, client, "/receipts/search?expense=demo1&preset=year"))
	if !strings.Contains(search, "schon verknüpft mit EX-0042") || strings.Contains(search, `name="docs" value="202"`) {
		t.Fatalf("taken scan offered:\n%s", search)
	}
	if search := string(mustGet(t, srv, client, "/receipts/search?expense=demo1&preset=year&unlinked=1")); strings.Contains(search, "Kabelwerk") {
		t.Fatalf("taken scan despite unlinked only:\n%s", search)
	}
	linked := string(mustGet(t, srv, client, "/receipts/part?tab=linked"))
	if !strings.Contains(linked, "EX-0042") || !strings.Contains(linked, "Rechnung Kabelwerk Studiobedarf") {
		t.Fatalf("linked lacks Kabelwerk:\n%s", linked)
	}
	fields := string(mustGet(t, srv, client, "/receipts/part?tab=fields"))
	if !regexp.MustCompile(`<option value="2" selected>Paperless \(Feld 2\) · bei 1 Ausgaben gefüllt</option>`).MatchString(fields) {
		t.Fatalf("mapping form:\n%s", fields)
	}
	for _, dup := range []string{"Invoice Ninja · Invoice Ninja", "Paperless-ngx · Paperless", "(string)", "receipts."} {
		if strings.Contains(fields, dup) {
			t.Fatalf("mapping form repeats or leaks %q:\n%s", dup, fields)
		}
	}
	if !strings.Contains(fields, "Invoice Ninja (Text)") && !strings.Contains(fields, "Invoice Ninja (URL)") {
		t.Fatalf("field types not translated:\n%s", fields)
	}

	// B: a combo search on request; C: scans picked by hand, with a sum,
	// linked together (the demo refuses the write, but only then).
	combos := string(mustGet(t, srv, client, "/receipts/combos?expense=demo5"))
	if !strings.Contains(combos, "2 Belege") || !strings.Contains(combos, "Summe 164,90") {
		t.Fatalf("combo search:\n%s", combos)
	}
	picked := string(mustGet(t, srv, client, "/receipts/search?expense=demo5&preset=vendor&unlinked=1"))
	for _, want := range []string{`name="doc" value="204"`, `data-amount="99"`, `data-target="164.9"`, "Ausgewählte verknüpfen"} {
		if !strings.Contains(picked, want) {
			t.Fatalf("hand-made combo lacks %q:\n%s", want, picked)
		}
	}
	resp = postForm(t, client, srv.URL+"/receipts/link", url.Values{"csrf": {csrfToken(t, srv, client)}, "expense": {"demo5"}, "doc": {"204", "205"}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=receipts.err_demo") {
		t.Fatalf("hand-made combo: %s", loc)
	}

	// Demo connections take no writes; the page says so.
	resp = postForm(t, client, srv.URL+"/receipts/link", url.Values{"csrf": {csrfToken(t, srv, client)}, "expense": {"demo1"}, "docs": {"201"}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=receipts.err_demo") {
		t.Fatalf("link on demo: %s", loc)
	}

	// Ignoring hides the expense from the suggestions until shown again.
	postForm(t, client, srv.URL+"/receipts/ignore", url.Values{"csrf": {csrfToken(t, srv, client)}, "kind": {"expense"}, "id": {"demo1"}, "reason": {"private"}})
	if part := string(mustGet(t, srv, client, "/receipts/part?tab=match")); strings.Contains(part, "EX-0041") {
		t.Fatal("ignored expense still suggested")
	}
	if ignored := string(mustGet(t, srv, client, "/receipts/part?tab=ignored")); !strings.Contains(ignored, "EX-0041") || !strings.Contains(ignored, "Privat") {
		t.Fatalf("ignored list:\n%s", ignored)
	}
	postForm(t, client, srv.URL+"/receipts/ignore", url.Values{"csrf": {csrfToken(t, srv, client)}, "kind": {"expense"}, "id": {"demo1"}, "show": {"1"}})
	if part := string(mustGet(t, srv, client, "/receipts/part?tab=match")); !strings.Contains(part, "EX-0041") {
		t.Fatal("expense not back after showing it again")
	}
}

// receiptsDemo sets up the demo Invoice Ninja and Paperless for the
// receipts page and picks them.
func receiptsDemo(t *testing.T) (srvURL string, get func(string) string, post func(string, url.Values) *http.Response) {
	t.Helper()
	srv, client := receiptsClient(t)
	get = func(path string) string { return string(mustGet(t, srv, client, path)) }
	post = func(path string, v url.Values) *http.Response {
		v.Set("csrf", csrfToken(t, srv, client))
		return postForm(t, client, srv.URL+path, v)
	}
	return srv.URL, get, post
}

// receiptsClient is a logged-in admin with demo Invoice Ninja and
// Paperless picked for the receipts.
func receiptsClient(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	conns := map[string]string{}
	for _, svc := range []string{"invoiceninja", "paperless"} {
		resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {svc}, "name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "tls": {"verify"}})
		conns[svc] = regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	}
	postForm(t, client, srv.URL+"/receipts/pick", url.Values{"csrf": {csrfToken(t, srv, client)}, "ninja": {conns["invoiceninja"]}, "paperless": {conns["paperless"]}})
	return srv, client
}

// chipCount reads the number of a tab or year chip from a part.
func chipCount(t *testing.T, part, id string) string {
	t.Helper()
	m := regexp.MustCompile(`id="` + id + `"[^>]*>[^<]*<span>(\d+)</span>`).FindStringSubmatch(part)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestReceiptCountsMatchLists: every number on the page counts what the
// list it leads to shows. The match tab's "scans without expense" link
// leads to "receipts first" and counts its scans; the year chips count
// the open tab's list.
func TestReceiptCountsMatchLists(t *testing.T) {
	_, get, _ := receiptsDemo(t)
	year := strconv.Itoa(time.Now().Year())

	queue := get("/receipts/part?tab=queue&year=" + year)
	scans := strings.Count(queue, `class="receipt-expense receipt-doc"`)
	if scans == 0 || chipCount(t, queue, "receipts-tab-queue") != strconv.Itoa(scans) {
		t.Fatalf("queue chip %q, list %d:\n%s", chipCount(t, queue, "receipts-tab-queue"), scans, queue)
	}
	if got := chipCount(t, queue, "receipts-year-"+year); got != "" && got != strconv.Itoa(scans) {
		t.Fatalf("queue year chip %q, list %d", got, scans)
	}

	match := get("/receipts/part?tab=match&year=" + year)
	if want := ">" + strconv.Itoa(scans) + " Belege ohne Ausgabe"; !strings.Contains(match, want) {
		t.Fatalf("match links %q scans, the queue lists %d:\n%s", regexp.MustCompile(`>\d+ Belege ohne Ausgabe`).FindString(match), scans, match)
	}

	linked := get("/receipts/part?tab=linked&year=" + year)
	rows := chipCount(t, linked, "receipts-tab-linked")
	if got := chipCount(t, linked, "receipts-year-"+year); got != rows {
		t.Fatalf("linked year chip %q, tab %q:\n%s", got, rows, linked)
	}
}

// TestReceiptLinkManyRefused: a refused bulk link shows the refusal
// only, not "linked: 0" as a success.
func TestReceiptLinkManyRefused(t *testing.T) {
	_, get, post := receiptsDemo(t)
	resp := post("/receipts/link-many", url.Values{"pair": {"demo1:201"}})
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=receipts.err_demo") || strings.Contains(loc, "linked=") {
		t.Fatalf("refused bulk link: %s", loc)
	}
	if page := get("/receipts?linked=0"); strings.Contains(page, "Verknüpft: 0") {
		t.Fatal("zero links shown as a success")
	}
}

// TestReceiptTabSurvivesCreate: "new expense" from "receipts first"
// returns there, as do the expense search and its links.
func TestReceiptTabSurvivesCreate(t *testing.T) {
	_, get, post := receiptsDemo(t)
	year := strconv.Itoa(time.Now().Year())
	queue := get("/receipts/part?tab=queue&year=" + year)
	for _, path := range []string{"/receipts/new-expense?", "/receipts/expenses?"} {
		m := regexp.MustCompile(`hx-get="(` + regexp.QuoteMeta(path) + `[^"]*)"`).FindStringSubmatch(queue)
		if m == nil || !strings.Contains(m[1], "tab=queue") {
			t.Fatalf("%s loses the tab: %v", path, m)
		}
	}
	form := get("/receipts/new-expense?doc=206&tab=queue&year=" + year)
	if !strings.Contains(form, `name="tab" value="queue"`) {
		t.Fatalf("form loses the tab:\n%s", form)
	}
	resp := post("/receipts/new-expense", url.Values{"doc": {"206"}, "tab": {"queue"}, "year": {year}, "amount": {"72,14"}, "day": {year + "-10-01"}, "vendor": {"Tankstelle"}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "tab=queue") {
		t.Fatalf("create returns to %s", loc)
	}
}

// A new expense refused in its panel comes back there with the error
// and what was typed; the page stays.
func TestReceiptCreateKeepsInput(t *testing.T) {
	srv, client := receiptsClient(t)
	year := strconv.Itoa(time.Now().Year())
	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "doc": {"206"}, "tab": {"queue"}, "year": {year},
		"amount": {"72.14"}, "day": {year + "-10-01"}, "vendor": {"Typed Vendor"}, "notes": {"Typed note"}}
	status, body := browse(t, client, http.MethodPost, srv.URL+"/receipts/new-expense", form, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("refusal in the panel: %d", status)
	}
	for _, want := range []string{i18n.T("receipts.err_demo", enums.LocaleDE, nil), `value="Typed Vendor"`, `value="Typed note"`,
		`value="72.14"`, `value="` + year + `-10-01"`, `name="tab" value="queue"`, `hx-post="/receipts/new-expense"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing:\n%s", want, body)
		}
	}
}

// A field mapping refused in its tab comes back there with the error
// and the fields as chosen.
func TestReceiptFieldsKeepChoice(t *testing.T) {
	srv, client := receiptsClient(t)
	part := string(mustGet(t, srv, client, "/receipts/part?tab=fields"))
	ids := regexp.MustCompile(`(?s)name="field_invoice".*?<option value="(\d+)".*?<option value="(\d+)"`).FindStringSubmatch(part)
	if ids == nil {
		t.Fatalf("no fields to choose:\n%s", part)
	}
	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "tab": {"fields"},
		"field_invoice": {ids[2]}, "field_expense": {ids[2]}, "field_link": {ids[1]}}
	status, body := browse(t, client, http.MethodPost, srv.URL+"/receipts/fields", form, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK || !strings.Contains(body, i18n.T("receipts.err_field_twice", enums.LocaleDE, nil)) {
		t.Fatalf("refusal in the tab: %d\n%s", status, body)
	}
	for _, name := range []string{"field_invoice", "field_expense"} {
		sel := regexp.MustCompile(`(?s)name="` + name + `".*?</select>`).FindString(body)
		if !strings.Contains(sel, `<option value="`+ids[2]+`" selected`) {
			t.Errorf("%s not kept as chosen:\n%s", name, sel)
		}
	}
}
