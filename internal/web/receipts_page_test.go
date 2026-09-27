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

	combos := string(mustGet(t, srv, client, "/receipts/part?tab=match&combo=1"))
	if !strings.Contains(combos, "2 Belege") {
		t.Fatalf("no 1∶n combo for the two Mietwagen Nord receipts:\n%s", combos)
	}
	queue := string(mustGet(t, srv, client, "/receipts/part?tab=queue"))
	if !strings.Contains(queue, "Tankquittung") {
		t.Fatalf("queue lacks the tagged scan:\n%s", queue)
	}
	linked := string(mustGet(t, srv, client, "/receipts/part?tab=linked"))
	if !strings.Contains(linked, "EX-0042") || !strings.Contains(linked, "Rechnung Kabelwerk Studiobedarf") {
		t.Fatalf("linked lacks Kabelwerk:\n%s", linked)
	}
	fields := string(mustGet(t, srv, client, "/receipts/part?tab=fields"))
	if !regexp.MustCompile(`<option value="2" selected>custom_value2 · Paperless</option>`).MatchString(fields) {
		t.Fatalf("mapping form:\n%s", fields)
	}

	// Demo connections take no writes; the page says so.
	resp = postForm(t, client, srv.URL+"/receipts/link", url.Values{"csrf": {csrfToken(t, srv, client)}, "expense": {"demo1"}, "docs": {"201"}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=receipts.err_demo") {
		t.Fatalf("link on demo: %s", loc)
	}

	// Ignoring hides the expense from the suggestions until shown again.
	postForm(t, client, srv.URL+"/receipts/ignore", url.Values{"csrf": {csrfToken(t, srv, client)}, "kind": {"expense"}, "id": {"demo1"}})
	if part := string(mustGet(t, srv, client, "/receipts/part?tab=match")); strings.Contains(part, "EX-0041") {
		t.Fatal("ignored expense still suggested")
	}
	if ignored := string(mustGet(t, srv, client, "/receipts/part?tab=ignored")); !strings.Contains(ignored, "EX-0041") {
		t.Fatalf("ignored list:\n%s", ignored)
	}
	postForm(t, client, srv.URL+"/receipts/ignore", url.Values{"csrf": {csrfToken(t, srv, client)}, "kind": {"expense"}, "id": {"demo1"}, "show": {"1"}})
	if part := string(mustGet(t, srv, client, "/receipts/part?tab=match")); !strings.Contains(part, "EX-0041") {
		t.Fatal("expense not back after showing it again")
	}
}
