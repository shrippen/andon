package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The Verbünde page: two Kimai make the space ambiguous; a Verbund made
// in the form settles it, shows in the connection's record, can lose a
// member and be deleted.
func TestVerbundPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, c := range []url.Values{
		{"service": {"kimai"}, "name": {"Kimai A"}, "url": {"demo://kimai/a"}},
		{"service": {"kimai"}, "name": {"Kimai B"}, "url": {"demo://kimai/b"}},
		{"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja"}},
	} {
		c.Set("csrf", csrfToken(t, srv, client))
		c.Set("space_id", space)
		c.Set("mode", "shared")
		c.Set("tls", "verify")
		postForm(t, client, srv.URL+"/connections", c)
	}
	page := string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	if !strings.Contains(page, "callout-warn") {
		t.Fatalf("no ambiguity notice:\n%s", page)
	}
	ids := map[string]string{}
	for _, m := range regexp.MustCompile(`name="conn" value="(\d+)"><span class="check-box">.*?</span> [^·]+· ([^<]+)<`).FindAllStringSubmatch(page, -1) {
		ids[strings.TrimSpace(m[2])] = m[1]
	}
	if ids["Kimai A"] == "" || ids["Ninja"] == "" {
		t.Fatalf("form connections: %v", ids)
	}

	// Two Kimai in one Verbund are refused.
	resp := postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"x"}, "conn": {ids["Kimai A"], ids["Kimai B"]}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "error=verbund.service_twice") {
		t.Fatalf("twice: %s", loc)
	}

	resp = postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"Firma A"}, "conn": {ids["Kimai A"], ids["Ninja"]}})
	if resp.StatusCode != http.StatusSeeOther || strings.Contains(resp.Header.Get("Location"), "error") {
		t.Fatalf("create: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	page = string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	if !strings.Contains(page, `value="Firma A"`) || strings.Contains(page, "callout-warn") {
		t.Fatalf("after create:\n%s", page)
	}
	record := string(mustGet(t, srv, client, "/connections/"+ids["Ninja"]))
	if !strings.Contains(record, "<b>Firma A</b>") || !strings.Contains(record, "Kimai A") {
		t.Fatal("record does not show the Verbund")
	}

	verbundID := regexp.MustCompile(`id="verbund-(\d+)"`).FindStringSubmatch(page)[1]
	resp = postForm(t, client, srv.URL+"/verbund/"+verbundID+"/members/"+ids["Ninja"]+"/remove", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove: %d", resp.StatusCode)
	}
	// One member left: the Verbund is gone, the space is ambiguous again.
	page = string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	if regexp.MustCompile(`id="verbund-\d`).MatchString(page) || !strings.Contains(page, "callout-warn") {
		t.Fatalf("after remove:\n%s", page)
	}
}
