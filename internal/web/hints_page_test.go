package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestHintsPageCompactAndBulk: hint cards lead with their action as a
// button and the reason as text, the note only on demand; a group's
// "all done" acknowledges every hint in it.
func TestHintsPageCompactAndBulk(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=invoiceninja"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"invoiceninja"}, "space_id": {string(space)},
		"name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/hints"))
	for _, want := range []string{`class="btn btn-accent btn-sm hint-go"`, `class="hint-why"`, `class="hint-note"`, `class="hint-bulk"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("hints page lacks %q:\n%s", want, page)
		}
	}
	if regexp.MustCompile(`<input name="note"[^>]*>\s*<button`).MatchString(page) {
		t.Fatal("note field still shown on every card")
	}

	form := regexp.MustCompile(`(?s)<form method="post" action="/hints/bulk" class="hint-bulk">(.*?)</form>`).FindStringSubmatch(page)
	ids := regexp.MustCompile(`name="id" value="(\d+)"`).FindAllStringSubmatch(form[1], -1)
	if len(ids) == 0 {
		t.Fatal("group form lists no hints")
	}
	values := url.Values{"csrf": {csrf}, "action": {"ack"}}
	for _, id := range ids {
		values.Add("id", id[1])
	}
	if res := postForm(t, client, srv.URL+"/hints/bulk", values); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("bulk: %d", res.StatusCode)
	}
	after := string(mustGet(t, srv, client, "/hints"))
	for _, id := range ids {
		if strings.Contains(after, `id="hint-`+id[1]+`"`) {
			t.Fatalf("hint %s still open after bulk ack", id[1])
		}
	}
}
