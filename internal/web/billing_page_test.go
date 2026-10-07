package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
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
