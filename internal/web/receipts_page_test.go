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

	// One Ninja, two Paperless, no Verbund: the page asks for one.
	page = string(mustGet(t, srv, client, "/receipts"))
	if !strings.Contains(page, "Welche gehören zusammen?") || strings.Contains(page, `id="receipts-part"`) {
		t.Fatalf("no Verbund asked for:\n%s", page)
	}

	// Two Verbünde: the first shows, the page offers the other; a pick sticks.
	for _, v := range []struct{ name, docs string }{{"Studio", "Archiv A"}, {"Verein", "Archiv B"}} {
		postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "name": {v.name}, "conn": {conns["Ninja"], conns[v.docs]}})
	}
	page = string(mustGet(t, srv, client, "/receipts"))
	if !strings.Contains(page, "Mehrere Verbünde passen") || !strings.Contains(page, ">Verein</option>") || !strings.Contains(page, `id="receipts-part"`) {
		t.Fatalf("no Verbund pick offered:\n%s", page)
	}
	key := regexp.MustCompile(`<option value="(v\d+)">Verein</option>`).FindStringSubmatch(page)
	if key == nil {
		t.Fatalf("no key for Verein:\n%s", page)
	}
	resp, err := client.Get(srv.URL + "/receipts?scope=" + key[1])
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("pick: %v %v", resp, err)
	}
	resp.Body.Close()
	page = string(mustGet(t, srv, client, "/receipts"))
	if strings.Contains(page, "Mehrere Verbünde passen") || !strings.Contains(page, `<option value="`+key[1]+`" selected>Verein</option>`) {
		t.Fatalf("pick not remembered:\n%s", page)
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

	// The 1∶n combo comes by itself, the sure match is picked for the
	// review, the tab chips get their numbers.
	for _, want := range []string{"2 Belege", `name="choice_demo1" value="link:201" form="hunt" data-link checked`, `name="case" value="demo1" form="hunt"`, `id="receipts-tab-match" href="/receipts?tab=match&amp;year=`, `hx-swap-oob="true"`} {
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
	if !strings.Contains(picked, `action="/receipts/review"`) || strings.Contains(picked, `action="/receipts/link"`) {
		t.Fatalf("a search links without review:\n%s", picked)
	}
	resp, err = client.PostForm(srv.URL+"/receipts/review", url.Values{"csrf": {csrfToken(t, srv, client)}, "expense": {"demo5"}, "doc": {"204", "205"}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("hand-made combo review: %v %v", resp, err)
	}
	if review := readBody(t, resp); !strings.Contains(review, `name="docs_demo5" value="204,205"`) {
		t.Fatalf("hand-made combo review:\n%s", review)
	}

	// Demo connections take no writes; the page says so.
	resp = postForm(t, client, srv.URL+"/receipts/apply", url.Values{"csrf": {csrfToken(t, srv, client)}, "pick": {"demo1"}, "act_demo1": {"link"}, "docs_demo1": {"201"}})
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

// TestReceiptHunt: the review spells out the picks and sends them back
// as they were; the demo refuses the write, and the page says so only.
func TestReceiptHunt(t *testing.T) {
	_, get, post := receiptsDemo(t)
	srv, client := receiptsClient(t)
	year := strconv.Itoa(time.Now().Year())
	resp, err := client.PostForm(srv.URL+"/receipts/review", url.Values{"csrf": {csrfToken(t, srv, client)}, "year": {year},
		"case": {"demo1", "demo5", "demo9"}, "choice_demo1": {"link:201"}, "choice_demo5": {""}, "choice_demo9": {"link:999"}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("review: %v %v", resp, err)
	}
	review := readBody(t, resp)
	for _, want := range []string{"Belegjagd bestätigen", "Schreibvorgänge: 1", `name="act_demo1" value="link"`, `name="docs_demo1" value="201"`} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	if strings.Contains(review, "act_demo5") || strings.Contains(review, "act_demo9") {
		t.Fatalf("review sends back a step without pick or with a problem:\n%s", review)
	}

	resp = post("/receipts/apply", url.Values{"year": {year}, "pick": {"demo1"}, "act_demo1": {"link"}, "docs_demo1": {"201"}})
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=receipts.err_demo") || strings.Contains(loc, "hunted=") {
		t.Fatalf("hunt on the demo: %s", loc)
	}
	if page := get("/receipts?hunted=0"); strings.Contains(page, "Erledigt: 0") {
		t.Fatal("zero steps shown as a success")
	}
}

// TestReceiptTabSurvivesCreate: "new expense" from "receipts first"
// returns there, as do the expense search and its links.
func TestReceiptTabSurvivesCreate(t *testing.T) {
	_, get, _ := receiptsDemo(t)
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
	if !strings.Contains(form, `action="/receipts/review"`) {
		t.Fatalf("new expense without review:\n%s", form)
	}
}

// A new expense is reviewed first; the review keeps what was typed and
// names a missing amount.
func TestReceiptCreateKeepsInput(t *testing.T) {
	srv, client := receiptsClient(t)
	year := strconv.Itoa(time.Now().Year())
	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "doc": {"206"}, "tab": {"queue"}, "year": {year},
		"amount": {"72.14"}, "day": {year + "-10-01"}, "vendor": {"Typed Vendor"}, "notes": {"Typed note"}}
	resp, err := client.PostForm(srv.URL+"/receipts/review", form)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("review: %v %v", resp, err)
	}
	body := readBody(t, resp)
	for _, want := range []string{`name="act_scan-206" value="create"`, `name="vendor_scan-206" value="Typed Vendor"`, `name="notes_scan-206" value="Typed note"`,
		`name="amount_scan-206" value="72.14"`, `name="day_scan-206" value="` + year + `-10-01"`, `name="tab" value="queue"`, "Neue Ausgabe bei Typed Vendor"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing:\n%s", want, body)
		}
	}

	// Without an amount the review names the problem and offers no write.
	form.Set("amount", "")
	resp, err = client.PostForm(srv.URL+"/receipts/review", form)
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, resp); !strings.Contains(body, i18n.T("receipts.err_amount", enums.LocaleDE, nil)) || strings.Contains(body, "act_scan-206") {
		t.Errorf("no amount:\n%s", body)
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
