package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestBenchDrafts: every workbench with a form of cases keeps a case's
// draft and puts it back on its next load, with the note. Not here:
// vendors (the demo has no duplicate vendors, so no case) and vat
// (TestBenchDraft).
func TestBenchDrafts(t *testing.T) {
	for _, c := range []struct {
		bench    string
		services []string
		based    bool // its proposals follow more than the case key
	}{
		{"euer", []string{"invoiceninja"}, false},
		{"week", []string{"kimai", "dawarich"}, true},
		{"inventory", []string{"snipeit", "fritzbox"}, false},
		{"inventory", []string{"kept", "fritzbox"}, false}, // no Snipe-IT: Open ends early
		{"dunning", []string{"invoiceninja"}, true},
		{"purchases", []string{"snipeit", "invoiceninja"}, false},
		{"subs", []string{"sure", "wallos", "invoiceninja"}, true},
		{"updates", []string{"gitea", "wud", "freshrss"}, true},
		{"categorize", []string{"sure"}, false},
		{"feeds", []string{"gitea", "freshrss"}, false},
	} {
		t.Run(c.bench, func(t *testing.T) {
			srv, client, code := newTestServer(t)
			setupAdmin(t, srv, client, code)
			login(t, srv, client)
			space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
			for _, service := range c.services {
				postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
					"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
			}
			if c.bench == "euer" {
				euerField(t, srv, client)
			}

			path := "/" + c.bench
			if c.bench == "week" {
				path = weekWithCase(t, srv, client)
			}
			page := string(mustGet(t, srv, client, path))
			k := regexp.MustCompile(`data-draft-case="([^"]+)"(?: data-draft-basis="([^"]*)")?`).FindStringSubmatch(page)
			at := regexp.MustCompile(`data-draft="/bench/` + c.bench + `/draft" data-draft-place="([^"]*)"`).FindStringSubmatch(page)
			if k == nil || at == nil {
				t.Fatalf("page lacks the draft's hooks:\n%s", page)
			}

			if c.based && (len(k) < 3 || k[2] == "") {
				t.Fatalf("case %s lacks its basis", k[1])
			}
			cases := `{"` + k[1] + `": {"note_` + k[1] + `": ["Entwurf"], "basis_` + k[1] + `": ["` + html.UnescapeString(k[2]) + `"]}}`
			resp := postForm(t, client, srv.URL+"/bench/"+c.bench+"/draft", url.Values{"csrf": {csrfToken(t, srv, client)}, "place": {at[1]}, "cases": {cases}})
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("keep: %d", resp.StatusCode)
			}
			page = html.UnescapeString(string(mustGet(t, srv, client, path)))
			for _, want := range []string{`"note_` + k[1] + `":["Entwurf"]`, "Zwischenstand geladen: 1 Fälle"} {
				if !strings.Contains(page, want) {
					t.Fatalf("page lacks %q:\n%s", want, page)
				}
			}
		})
	}
}

// weekWithCase is a week holding the demo's visit without a Kimai entry:
// a weekday back from today, so which week depends on the day the test runs.
func weekWithCase(t *testing.T, srv *httptest.Server, client *http.Client) string {
	t.Helper()
	page := string(mustGet(t, srv, client, "/week"))
	for _, m := range regexp.MustCompile(`href="(/week\?[^"]*week=[^"]*)"`).FindAllStringSubmatch(page, -1) {
		link := html.UnescapeString(m[1])
		if strings.Contains(string(mustGet(t, srv, client, link)), "data-draft-case=") {
			return link
		}
	}
	t.Fatalf("no week holds a case:\n%s", page)
	return ""
}

// euerField picks the demo Invoice Ninja's EÜR field, without which the
// EÜR bench offers nothing to write.
func euerField(t *testing.T, srv *httptest.Server, client *http.Client) {
	t.Helper()
	page := string(mustGet(t, srv, client, "/euer"))
	m := regexp.MustCompile(`href="(/categories\?ninja=\d+&amp;bank=0)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no categories link:\n%s", page)
	}
	link := strings.ReplaceAll(m[1], "&amp;", "&")
	postForm(t, client, srv.URL+link, url.Values{"csrf": {csrfToken(t, srv, client)}, "euer_slot": {"3"}})
}

// TestBenchDraftBasis: a reminder drafted at another stage does not come
// back: its subject and body were that stage's.
func TestBenchDraftBasis(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})

	page := string(mustGet(t, srv, client, "/dunning"))
	m := regexp.MustCompile(`data-draft-case="([^"]+)" data-draft-basis="([^"]+)"`).FindStringSubmatch(page)
	at := regexp.MustCompile(`data-draft-place="([^"]*)"`).FindStringSubmatch(page)
	if m == nil || at == nil {
		t.Fatalf("reminder lacks its basis:\n%s", page)
	}
	k, stage := m[1], m[2]

	keep := func(basis string) string {
		cases := `{"` + k + `": {"subject_` + k + `": ["Text der Stufe"], "basis_` + k + `": ["` + basis + `"]}}`
		postForm(t, client, srv.URL+"/bench/dunning/draft", url.Values{"csrf": {csrfToken(t, srv, client)}, "place": {at[1]}, "cases": {cases}})
		return html.UnescapeString(string(mustGet(t, srv, client, "/dunning")))
	}
	if page := keep("0"); strings.Contains(page, "Text der Stufe") || strings.Contains(page, "Zwischenstand geladen") {
		t.Fatalf("draft of another stage back:\n%s", page)
	}
	if page := keep(stage); !strings.Contains(page, "Text der Stufe") {
		t.Fatalf("draft of this stage lost:\n%s", page)
	}
}
