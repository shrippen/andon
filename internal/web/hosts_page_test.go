package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestHostsFoldQuiet: hosts with hints count them as problems and stay in
// the table; hosts without monitor or hint fold under "N ohne Befund".
func TestHostsFoldQuiet(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, svc := range []string{"kimai", "kdestore"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/hosts"))
	table, quiet, ok := strings.Cut(page, `<details class="fold`)
	if !ok {
		t.Fatalf("no fold for quiet hosts:\n%s", page)
	}
	if !strings.Contains(quiet, "1 ohne Befund") || !strings.Contains(quiet, `href="/hosts/kdestore"`) {
		t.Fatalf("quiet fold:\n%s", quiet)
	}
	if !regexp.MustCompile(`href="/hosts/kimai".*?<span class="late">\d+</span>`).MatchString(strings.ReplaceAll(table, "\n", " ")) {
		t.Fatalf("kimai's hints are no problems:\n%s", table)
	}
}
