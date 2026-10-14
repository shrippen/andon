package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestVATPage: without Invoice Ninja the page says so; with the demo it
// lists the open transactions, the review spells out the writes, and
// the demo refuses to write them.
func TestVATPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/vat"))
	if !strings.Contains(page, "Invoice Ninja fehlt") {
		t.Fatalf("no hint about the missing connection:\n%s", page)
	}

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}

	month := time.Now().UTC().Format("2006-01")
	page = string(mustGet(t, srv, client, "/vat?span="+month))
	for _, want := range []string{"Offene Umsätze", "Kabelwerk Studiobedarf", `name="act_demo-b-studio"`, "Zahllast"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}

	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "pick": {"demo-b-studio"},
		"act_demo-b-studio": {"create"}, "vendor_demo-b-studio": {"Kabelwerk Studiobedarf"}, "tax_demo-b-studio": {"19"}}
	resp, err := client.PostForm(srv.URL+"/vat/review", form)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("review: %v %v", resp, err)
	}
	review := readBody(t, resp)
	if !strings.Contains(review, "Ausgabe „Kabelwerk Studiobedarf“ anlegen") || !strings.Contains(review, "Vorsteuer 143,54") {
		t.Fatalf("review lacks the step:\n%s", review)
	}

	// "Archive all as private" takes the private accounts' rest only.
	all := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "all_private": {"1"}, "apart": {"demo-p-food"},
		"pick": {"demo-b-studio"}, "act_demo-b-studio": {"create"}}
	resp, err = client.PostForm(srv.URL+"/vat/review", all)
	if err != nil {
		t.Fatal(err)
	}
	if review := readBody(t, resp); !strings.Contains(review, "Als privat in Invoice Ninja archivieren") || strings.Contains(review, "Kabelwerk") {
		t.Fatalf("archive review:\n%s", review)
	}

	resp = postForm(t, client, srv.URL+"/vat/apply", form)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "vatcheck.err_demo") {
		t.Fatalf("apply on the demo: %d %s", resp.StatusCode, loc)
	}
}

// TestVerbundAccounts: the settled Verbund lists the demo's accounts;
// a role set there reaches /vat.
func TestVerbundAccounts(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	resp := postForm(t, client, srv.URL+"/spaces/"+space+"/verbund/settle", url.Values{"csrf": {csrfToken(t, srv, client)}})
	link := regexp.MustCompile(`/verbund/(\d+)/`).FindStringSubmatch(resp.Header.Get("Location"))
	if link == nil {
		t.Fatalf("settle: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	page := string(mustGet(t, srv, client, "/verbund/"+link[1]+"/accounts?space="+space))
	for _, want := range []string{"Geschäftskonto", "Privatkonto", "Tagesgeld", `name="role"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("accounts page lacks %q:\n%s", want, page)
		}
	}
	conn := regexp.MustCompile(`name="conn" value="(\d+)"><input type="hidden" name="key" value="demo-a2"`).FindStringSubmatch(page)
	if conn == nil {
		t.Fatalf("no role form for the private account:\n%s", page)
	}
	resp = postForm(t, client, srv.URL+"/verbund/"+link[1]+"/accounts/role", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space},
		"conn": {conn[1]}, "key": {"demo-a2"}, "role": {"private"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("role: %d", resp.StatusCode)
	}
	vat := string(mustGet(t, srv, client, "/vat?span="+time.Now().UTC().Format("2006-01")))
	if !regexp.MustCompile(`name="role:` + conn[1] + `:demo-a2"[^>]*>\s*<option value="">[^<]*</option>\s*<option value="business">[^<]*</option>\s*<option value="private" selected`).MatchString(vat) {
		t.Fatalf("/vat does not show the role:\n%s", vat)
	}
}

// TestVATScanOffer: a debit without expense, whose scan waits in
// Paperless, is offered the scan for its new expense.
func TestVATScanOffer(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "paperless"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	now := time.Now().UTC()
	for _, month := range []string{now.Format("2006-01"), now.AddDate(0, -1, 0).Format("2006-01")} {
		if page := string(mustGet(t, srv, client, "/vat?span="+month)); strings.Contains(page, `name="scan_demo-b-scan"`) {
			return
		}
	}
	t.Fatal("no scan offered for the debit of a waiting scan")
}
