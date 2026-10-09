package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// TestServicePickSortedWithCounts: the service picker lists services by
// their shown name within each topic and badges how many connections of
// each exist.
func TestServicePickSortedWithCounts(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	pick := string(mustGet(t, srv, client, "/connections/new"))
	total := 0
	for _, group := range regexp.MustCompile(`(?s)<section class="gal-group".*?</section>`).FindAllString(pick, -1) {
		names := regexp.MustCompile(`<a class="option"[^>]*><b>([^<]+)</b>`).FindAllStringSubmatch(group, -1)
		shown := make([]string, len(names))
		for i, m := range names {
			shown[i] = m[1]
		}
		total += len(shown)
		sorted := append([]string(nil), shown...)
		collate.New(language.German, collate.IgnoreCase).SortStrings(sorted)
		if strings.Join(shown, "|") != strings.Join(sorted, "|") {
			t.Fatalf("not alphabetical:\n%v", shown)
		}
	}
	if total < 10 {
		t.Fatalf("picker lists %d services:\n%s", total, pick)
	}

	form := string(mustGet(t, srv, client, "/connections/new?service=kimai"))
	space := regexp.MustCompile(`<option value="(\d+)">`).FindStringSubmatch(form)[1]
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	postForm(t, &noFollow, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space}, "service": {"kimai"},
		"name": {"K"}, "url": {"http://127.0.0.1:1"}, "mode": {"shared"}, "secret": {"tok"}, "tls": {"verify"}})

	pick = string(mustGet(t, srv, client, "/connections/new"))
	if !regexp.MustCompile(`href="/connections/new\?service=kimai"><b>Kimai<span class="count"[^>]*>1</span></b>`).MatchString(pick) {
		t.Fatalf("kimai card lacks its count:\n%s", pick)
	}
}

// TestServicePickExperimental: services not yet tested against a real
// instance carry a badge in the picker and a note on their form; partly
// tested ones only the note.
func TestServicePickExperimental(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	pick := string(mustGet(t, srv, client, "/connections/new"))
	badged := regexp.MustCompile(`service=(\w+)[^>]*><b>[^<]*(?:<span class="count"[^<]*</span>)?</b><span class="pill" data-state="locked">`).FindAllStringSubmatch(pick, -1)
	var names []string
	for _, m := range badged {
		names = append(names, m[1])
	}
	if !slices.Contains(names, "tibber") || slices.Contains(names, "fediverse") || slices.Contains(names, "kimai") {
		t.Fatalf("badged: %v", names)
	}

	for svc, note := range map[string]string{"tibber": "nur gegen Nachbauten", "fediverse": "Akkoma", "esphome": "Basic Auth"} {
		page := string(mustGet(t, srv, client, "/connections/new?service="+svc))
		if !strings.Contains(page, note) || strings.Contains(page, "conn.experimental") {
			t.Fatalf("%s: note %q missing", svc, note)
		}
	}
	if strings.Contains(string(mustGet(t, srv, client, "/connections/new?service=kimai")), `data-state="locked">Experimentell`) {
		t.Fatal("kimai marked experimental")
	}
}
