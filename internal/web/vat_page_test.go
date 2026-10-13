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
