package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
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

	// Two Kimai in one Verbund are refused; the form keeps what was typed.
	status, refused := browse(t, client, http.MethodPost, srv.URL+"/verbund",
		url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"Typed name"}, "conn": {ids["Kimai A"], ids["Kimai B"]}}, nil)
	if status != http.StatusBadRequest || !strings.Contains(refused, i18n.T("verbund.service_twice", enums.LocaleDE, nil)) ||
		!strings.Contains(refused, `value="Typed name"`) {
		t.Fatalf("twice: %d\n%s", status, refused)
	}

	resp := postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"Firma A"}, "conn": {ids["Kimai A"], ids["Ninja"]}})
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

	// A refused member answers with the page and its error, no redirect.
	status, refused = browse(t, client, http.MethodPost, srv.URL+"/verbund/"+verbundID+"/members",
		url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "conn": {ids["Kimai B"]}}, nil)
	if status != http.StatusBadRequest || !strings.Contains(refused, i18n.T("verbund.service_twice", enums.LocaleDE, nil)) {
		t.Fatalf("second Kimai as member: %d\n%s", status, refused)
	}
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
		{"service": {"sure"}, "name": {"Sure"}, "url": {"demo://sure/cust"}},
		{"service": {"paperless"}, "name": {"Paperless"}, "url": {"demo://paperless/cust"}},
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

	// Ninja leads; Kimai, Sure and Paperless are columns.
	for _, head := range []string{"<th>Invoice Ninja</th><th>Kimai</th><th>Sure", "<th>Paperless"} {
		if !strings.Contains(customers, head) {
			t.Fatalf("no %q:\n%s", head, customers)
		}
	}

	// One cell: "–" → suggested again, "no counterpart" → locked, a
	// customer → applied.
	cell := regexp.MustCompile(`customers/link" class="inline-form">\s*<input[^>]*><input[^>]*><input type="hidden" name="hub" value="([^"]+)"><input type="hidden" name="conn" value="(\d+)">`).FindStringSubmatch(customers)
	if cell == nil {
		t.Fatalf("no unlink form:\n%s", customers)
	}
	hub, conn := cell[1], cell[2]
	act := func(name string, extra url.Values) string {
		v := url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "hub": {hub}, "conn": {conn}}
		for k, x := range extra {
			v[k] = x
		}
		// Done: back to the page; refused: the page with the cell as chosen.
		status, body := browse(t, client, http.MethodPost, srv.URL+"/verbund/"+id+"/customers/"+name, v, nil)
		if status == http.StatusOK {
			t.Fatalf("%s answered 200, not a redirect", name)
		}
		if status < http.StatusBadRequest {
			return string(mustGet(t, srv, client, strings.ReplaceAll(link[1], "&amp;", "&")))
		}
		return body
	}
	// The cell's own key, and one another row holds in this column.
	form := regexp.MustCompile(`name="hub" value="` + regexp.QuoteMeta(hub) + `"><input type="hidden" name="conn" value="` + conn + `">\s*<select[^>]*>([\s\S]*?)</select>`).FindStringSubmatch(customers)
	if form == nil {
		t.Fatalf("no select for %s/%s", hub, conn)
	}
	own := regexp.MustCompile(`<option value="([^"!]+)" selected`).FindStringSubmatch(form[1])
	if own == nil {
		t.Fatalf("no selected key: %s", form[1])
	}
	if got := act("link", url.Values{"party": {""}}); !strings.Contains(got, `data-state="reviewing"`) {
		t.Fatalf("unlink did not bring the suggestion back:\n%s", got)
	}
	if got := act("link", url.Values{"party": {"!none"}}); !strings.Contains(got, `data-state="locked"`) {
		t.Fatalf("none not stored:\n%s", got)
	}
	taken := ""
	for _, m := range regexp.MustCompile(`<option value="([^"]+)"`).FindAllStringSubmatch(form[1], -1) {
		if m[1] != own[1] && m[1] != "!none" && strings.Contains(customers, `name="conn" value="`+conn+`">`) {
			taken = m[1]
			break
		}
	}
	got := act("link", url.Values{"party": {taken}})
	if !strings.Contains(got, "schon einem anderen Kunden") {
		t.Fatalf("taken key %s accepted:\n%s", taken, got)
	}
	// The refused cell keeps the choice, not the stored link.
	refused := regexp.MustCompile(`name="hub" value="` + regexp.QuoteMeta(hub) + `"><input type="hidden" name="conn" value="` + conn + `">\s*<select[^>]*>([\s\S]*?)</select>`).FindStringSubmatch(got)
	if refused == nil || !strings.Contains(refused[1], `value="`+taken+`" selected`) {
		t.Fatalf("refused choice %s not kept:\n%v\n%s", taken, refused, got)
	}
	if got := act("link", url.Values{"party": {own[1]}}); strings.Contains(got, `class="error"`) {
		t.Fatalf("link failed:\n%s", got)
	}
	if got := act("link", url.Values{"party": {"nope"}}); !strings.Contains(got, `class="error"`) {
		t.Fatalf("unknown key accepted:\n%s", got)
	}
}
