package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
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
		conns[c.name] = regexp.MustCompile(`/connections/(\d+)/edit`).FindStringSubmatch(resp.Header.Get("Location"))[1]
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
