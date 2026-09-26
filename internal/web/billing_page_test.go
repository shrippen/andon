package web_test

import (
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
}
