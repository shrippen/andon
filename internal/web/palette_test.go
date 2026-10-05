package web_test

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestPaletteFindsConnectionsAndHints: besides boards and pages the
// palette offers connections (to their form, with their state), rules
// with open hints (with a count) and settings, each under a heading.
func TestPaletteFindsConnectionsAndHints(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=invoiceninja"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"invoiceninja"}, "space_id": {string(space)},
		"name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	runAnalysis(t, srv)

	var items []struct{ Kind, Title, URL, Detail, Group string }
	if err := json.Unmarshal(mustGet(t, srv, client, "/palette.json"), &items); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for i, it := range items {
		if kinds[it.Kind] && items[i-1].Kind != it.Kind {
			t.Fatalf("group %q split: %+v", it.Kind, items)
		}
		kinds[it.Kind] = true
		if it.Group == "" {
			t.Fatalf("item without heading: %+v", it)
		}
		if it.Kind == "connection" && (it.Title != "Ninja" || !strings.HasPrefix(it.URL, "/connections/") || it.Detail == "") {
			t.Fatalf("connection item: %+v", it)
		}
		if it.Kind == "hints" && (!strings.HasPrefix(it.URL, "/hints#rule-") || it.Detail == "") {
			t.Fatalf("hints item: %+v", it)
		}
	}
	for _, want := range []string{"connection", "hints", "page", "setting"} {
		if !kinds[want] {
			t.Fatalf("palette lacks %q: %+v", want, items)
		}
	}
	if !strings.Contains(string(mustGet(t, srv, client, "/hints")), `id="rule-`) {
		t.Fatal("hint groups have no anchor")
	}
}
