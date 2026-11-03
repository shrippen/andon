package web_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestBenchDraft: a case kept by the VAT page comes back on its next
// load with a note; a field of another case, an unknown bench and a
// discarded draft are refused or gone.
func TestBenchDraft(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}

	page := string(mustGet(t, srv, client, "/vat"))
	m := regexp.MustCompile(`data-draft-place="([^"]*)"`).FindStringSubmatch(page)
	if m == nil || !strings.Contains(page, `data-draft-case="demo-b-studio"`) {
		t.Fatalf("page lacks the draft's hooks:\n%s", page)
	}
	place := m[1]

	keep := func(bench, cases string) int {
		t.Helper()
		resp := postForm(t, client, srv.URL+"/bench/"+bench+"/draft", url.Values{"csrf": {csrfToken(t, srv, client)}, "place": {place}, "cases": {cases}})
		return resp.StatusCode
	}
	if got := keep("vat", `{"demo-b-studio": {"pick": ["demo-b-studio"], "vendor_demo-b-studio": ["Lichtwerk"]}}`); got != http.StatusNoContent {
		t.Fatalf("keep: %d", got)
	}
	for name, c := range map[string]struct {
		bench, cases string
		want         int
	}{
		"other case's field": {"vat", `{"demo-b-studio": {"vendor_demo-p-food": ["x"]}}`, http.StatusBadRequest},
		"other case's pick":  {"vat", `{"demo-b-studio": {"pick": ["demo-p-food"]}}`, http.StatusBadRequest},
		"no JSON":            {"vat", `nope`, http.StatusBadRequest},
		"sealed case key":    {"vat", `{"a_enc": {"pick": ["a_enc"]}}`, http.StatusBadRequest},
		"sealed field name":  {"vat", `{"enc": {"x_enc": ["1"]}}`, http.StatusBadRequest},
		"unknown bench":      {"nope", `{}`, http.StatusNotFound},
	} {
		if got := keep(c.bench, c.cases); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}

	page = html.UnescapeString(string(mustGet(t, srv, client, "/vat")))
	for _, want := range []string{`"vendor_demo-b-studio":["Lichtwerk"]`, "Zwischenstand geladen: 1 Fälle"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}

	resp := postForm(t, client, srv.URL+"/bench/vat/draft/drop", url.Values{"csrf": {csrfToken(t, srv, client)}, "place": {place}})
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "/vat?note=workbench.draft_dropped") {
		t.Fatalf("drop: %d %s", resp.StatusCode, loc)
	}
	if page := string(mustGet(t, srv, client, "/vat")); strings.Contains(page, "Lichtwerk") {
		t.Fatal("dropped draft back")
	}
}
