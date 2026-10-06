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

// The customers page of a Verbund: suggestions from the demo names,
// confirmed in one go.
func TestVerbundCustomersPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, c := range []url.Values{
		{"service": {"kimai"}, "name": {"Kimai"}, "url": {"demo://kimai/cust"}},
		{"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja/cust"}},
	} {
		c.Set("csrf", csrfToken(t, srv, client))
		c.Set("space_id", space)
		c.Set("mode", "shared")
		c.Set("tls", "verify")
		postForm(t, client, srv.URL+"/connections", c)
	}
	page := string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	var conns []string
	for _, m := range regexp.MustCompile(`name="conn" value="(\d+)"`).FindAllStringSubmatch(page, -1) {
		conns = append(conns, m[1])
	}
	postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"Studio"}, "conn": conns})
	page = string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	link := regexp.MustCompile(`href="(/verbund/\d+/customers\?space=\d+)"`).FindStringSubmatch(page)
	if link == nil {
		t.Fatalf("no customers link:\n%s", page)
	}
	customers := string(mustGet(t, srv, client, strings.ReplaceAll(link[1], "&amp;", "&")))
	if !strings.Contains(customers, `data-state="reviewing"`) {
		t.Fatalf("no suggestions:\n%s", customers)
	}
	id := regexp.MustCompile(`/verbund/(\d+)/customers`).FindStringSubmatch(link[1])[1]
	postForm(t, client, srv.URL+"/verbund/"+id+"/customers/confirm", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}})
	customers = string(mustGet(t, srv, client, strings.ReplaceAll(link[1], "&amp;", "&")))
	if strings.Contains(customers, `data-state="reviewing"`) || !strings.Contains(customers, `data-state="applied"`) {
		t.Fatalf("not confirmed:\n%s", customers)
	}
	if strings.Contains(customers, "/customers/confirm") {
		t.Error("confirm button shown with nothing to confirm")
	}

	// One customer: unlink → suggested again, none → locked, link → applied.
	row := regexp.MustCompile(`/verbund/\d+/customers/(\d+)/unlink`).FindStringSubmatch(customers)
	if row == nil {
		t.Fatalf("no unlink form:\n%s", customers)
	}
	act := func(name string, extra url.Values) string {
		v := url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}}
		for k, x := range extra {
			v[k] = x
		}
		resp := postForm(t, client, srv.URL+"/verbund/"+id+"/customers/"+row[1]+"/"+name, v)
		return string(mustGet(t, srv, client, resp.Header.Get("Location")))
	}
	if got := act("unlink", nil); !strings.Contains(got, `data-state="reviewing"`) {
		t.Fatalf("unlink did not bring the suggestion back:\n%s", got)
	}
	if got := act("none", nil); !strings.Contains(got, `data-state="locked"`) {
		t.Fatalf("none not stored:\n%s", got)
	}
	// The clients other rows hold are taken; this row's own one is free.
	own := regexp.MustCompile(`customers/` + row[1] + `/link"[\s\S]*?<option value="([^"]+)" selected`).FindStringSubmatch(customers)
	if own == nil {
		t.Fatalf("no selected client in row %s:\n%s", row[1], customers)
	}
	for _, m := range regexp.MustCompile(`<option value="([^"]+)"`).FindAllStringSubmatch(customers, -1) {
		if m[1] == own[1] {
			continue
		}
		if got := act("link", url.Values{"client": {m[1]}}); !strings.Contains(got, "schon einem anderen Kimai-Kunden") {
			t.Fatalf("taken client %s accepted:\n%s", m[1], got)
		}
		break
	}
	if got := act("link", url.Values{"client": {own[1]}}); strings.Contains(got, `data-state="locked"`) || strings.Contains(got, `class="error"`) {
		t.Fatalf("link did not replace none:\n%s", got)
	}
	if got := act("link", url.Values{"client": {"nope"}}); !strings.Contains(got, `class="error"`) {
		t.Fatalf("unknown client accepted:\n%s", got)
	}
}
