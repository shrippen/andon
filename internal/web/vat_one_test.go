package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestVATOne: a desk's "review this one" spells out its case alone
// below the desk, with a button writing only it; on the demo the write
// is refused like any.
func TestVATOne(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	month := studioMonth()
	page := string(mustGet(t, srv, client, "/vat?span="+month))
	if !strings.Contains(page, `id="one-demo-b-studio"`) || !strings.Contains(page, `"only": "demo-b-studio"`) {
		t.Fatalf("desk lacks its own review:\n%s", page)
	}

	// The whole form goes, another case ticked; only the one is reviewed.
	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "only": {"demo-b-studio"},
		"pick": {"demo-b-host"}, "act_demo-b-host": {"skip"},
		"act_demo-b-studio": {"create"}, "vendor_demo-b-studio": {"Kabelwerk Studiobedarf"}, "tax_demo-b-studio": {"19"}}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/vat/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("review one: %v %v", resp, err)
	}
	one := readBody(t, resp)
	for _, want := range []string{"Ausgabe „Kabelwerk Studiobedarf“ anlegen", `formaction="/vat/apply"`, `name="only" value="demo-b-studio"`} {
		if !strings.Contains(one, want) {
			t.Fatalf("fragment lacks %q:\n%s", want, one)
		}
	}
	if strings.Contains(one, "<html") || strings.Contains(one, "archivieren") {
		t.Fatalf("fragment is a page or holds the other case:\n%s", one)
	}

	resp = postForm(t, client, srv.URL+"/vat/apply", form)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "vatcheck.err_demo") {
		t.Fatalf("apply one on the demo: %d %s", resp.StatusCode, loc)
	}
}
