package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBillingSummary: the billing page opens with what is waiting, each
// part a jump link, and every payment says how sure its match is.
func TestBillingSummary(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, svc := range []string{"kimai", "invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/billing"))
	for _, want := range []string{`class="billing-summary"`, `href="#drafts"`, `href="#payments"`, `href="#mails"`, `id="export"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("billing page lacks %q:\n%s", want, page)
		}
	}
	// The demo's customers exist in both: every draft can be created.
	if strings.Contains(page, "Kein Invoice-Ninja-Kunde") || strings.Contains(page, "No Invoice Ninja client") || !strings.Contains(page, `action="/billing/draft"`) {
		t.Fatalf("drafts without their Invoice Ninja client:\n%s", page)
	}
}

// TestBillingNamesOpenDraft: a customer whose client already has a draft
// in Invoice Ninja names it with a link before "create" (the demo's
// second client has one); creating stays possible.
func TestBillingNamesOpenDraft(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, svc := range []string{"kimai", "invoiceninja"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/billing"))
	open := regexp.MustCompile(`Offener Entwurf R-\d{4}-\d+ vom \d\d\.\d\d\.\d{4}`)
	if !open.MatchString(page) || !strings.Contains(page, `/#/invoices/`) {
		t.Fatalf("billing page does not name the open draft:\n%s", page)
	}
	if strings.Count(page, `action="/billing/draft"`) < 2 {
		t.Fatalf("creating a draft is no longer offered:\n%s", page)
	}
}

// TestMailForwardNamesMissingLogin: forwarding without the own Paperless
// login says which connection lacks it and links to where it is entered;
// the answer is a redirect, so reloading does not send again.
func TestMailForwardNamesMissingLogin(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := instanceSpace(t, srv, client)

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"mail"}, "space_id": {space},
		"name": {"Postfach"}, "url": {"demo://mail"}, "mode": {"shared"}, "secret": {"demo:demo"}, "tls": {"verify"}})
	mail := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	resp = postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"paperless"}, "space_id": {space},
		"name": {"Archiv"}, "url": {"demo://paperless"}, "mode": {"personal"}, "tls": {"verify"}})
	paperless := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]

	resp = postForm(t, client, srv.URL+"/billing/mail", url.Values{"csrf": {csrf}, "conn": {mail}, "uid": {"1"}})
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/billing?") {
		t.Fatalf("forward answered %d %q, want a redirect to /billing", resp.StatusCode, loc)
	}
	page := string(mustGet(t, srv, client, loc))
	link := `href="/connections/` + paperless + `?tab=access"`
	if !strings.Contains(page, link) || !strings.Contains(page, "Archiv") {
		t.Fatalf("page lacks the connection name and %s:\n%s", link, page)
	}
}

// TestMailForwardDemo: demo connections take no writes; the page says so
// after a redirect.
func TestMailForwardDemo(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := instanceSpace(t, srv, client)

	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"mail"}, "space_id": {space},
		"name": {"Postfach"}, "url": {"demo://mail"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	mail := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"paperless"}, "space_id": {space},
		"name": {"Archiv"}, "url": {"demo://paperless"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})

	resp = postForm(t, client, srv.URL+"/billing/mail", url.Values{"csrf": {csrf}, "conn": {mail}, "uid": {"1"}})
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forward answered %d, want a redirect", resp.StatusCode)
	}
	if page := string(mustGet(t, srv, client, loc)); !strings.Contains(page, "Demo-Verbindungen nehmen keine Änderungen an.") {
		t.Fatalf("page lacks the demo note:\n%s", page)
	}
}

// TestTablesFoldToCards: billing and client tables become cards on a
// phone (Kante table.cards-sm): every value cell names its column, and
// each row marks its amount for the top right of the card.
func TestTablesFoldToCards(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, svc := range []string{"kimai", "invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)

	clients := string(mustGet(t, srv, client, "/clients"))
	pages := map[string]string{"/billing": string(mustGet(t, srv, client, "/billing")), "/clients": clients}
	// The first client with invoices (a client page shows them as a table).
	for _, link := range regexp.MustCompile(`href="(/clients/\d+/\d+\?kimai=\d+)[^"]*"`).FindAllStringSubmatch(clients, -1) {
		page := string(mustGet(t, srv, client, link[1]))
		if strings.Contains(page, "<table") {
			pages["client"] = page
			break
		}
	}
	if pages["client"] == "" {
		t.Fatalf("no client page with invoices:\n%s", clients)
	}

	table := regexp.MustCompile(`(?s)<table class="table[^"]*">.*?</table>`)
	cell := regexp.MustCompile(`<td[^>]*>`)
	for name, page := range pages {
		tables := table.FindAllString(page, -1)
		if len(tables) == 0 {
			t.Fatalf("%s: no table:\n%s", name, page)
		}
		for _, tb := range tables {
			if !strings.HasPrefix(tb, `<table class="table cards-sm">`) {
				t.Fatalf("%s: table without cards-sm: %.120s", name, tb)
			}
			// The client list has several amounts: none goes top right
			// without its name (Kante's key cell shows no label).
			if name == "/clients" {
				if strings.Contains(tb, `data-card="key"`) {
					t.Fatalf("%s: an amount top right without its label: %s", name, tb)
				}
			} else if !strings.Contains(tb, `data-card="key"`) {
				t.Fatalf("%s: no amount marked for the card: %s", name, tb)
			}
			for _, td := range cell.FindAllString(tb, -1) {
				if strings.Contains(td, `class="num"`) && !strings.Contains(td, "data-label=") && !strings.Contains(td, `data-card="key"`) {
					t.Fatalf("%s: value cell without its column name: %s", name, td)
				}
			}
		}
	}
}

// TestBillingExportKeepsChoice: a refused year package shows the page
// with its error and the year as chosen, not this year again.
func TestBillingExportKeepsChoice(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := instanceSpace(t, srv, client)
	last := strconv.Itoa(time.Now().Year() - 1)

	status, body := browse(t, client, http.MethodGet, srv.URL+"/billing/export?space_id="+space+"&link_id=999&year="+last, nil, nil)
	if status != http.StatusBadRequest || !strings.Contains(body, `class="error"`) {
		t.Fatalf("refused export: %d\n%s", status, body)
	}
	if !strings.Contains(body, `<option value="`+last+`" selected`) {
		t.Fatalf("year %s not kept:\n%s", last, body)
	}
}
