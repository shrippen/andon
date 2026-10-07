package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestHintDoneComesBack: "done" keeps the place on the page (the next
// hint's anchor), the done view lists the hint as handled and reopens it.
func TestHintDoneComesBack(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=invoiceninja"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"invoiceninja"}, "space_id": {string(space)},
		"name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	runAnalysis(t, srv)

	page := string(mustGet(t, srv, client, "/hints"))
	ack := regexp.MustCompile(`(?s)action="/hints/(\d+)/ack".*?name="next" value="(\d+)"`).FindStringSubmatch(page)
	if ack == nil {
		t.Fatalf("no done form with the next hint:\n%s", page)
	}
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.PostForm(srv.URL+"/hints/"+ack[1]+"/ack", url.Values{"csrf": {csrf}, "next": {ack[2]}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "#hint-"+ack[2]) {
		t.Fatalf("back to %q", loc)
	}

	done := string(mustGet(t, srv, client, "/hints?view=done"))
	if !strings.Contains(done, `action="/hints/`+ack[1]+`/reopen"`) {
		t.Fatalf("done view cannot reopen:\n%s", done)
	}
	postForm(t, client, srv.URL+"/hints/"+ack[1]+"/reopen", url.Values{"csrf": {csrf}})
	if !strings.Contains(string(mustGet(t, srv, client, "/hints")), `action="/hints/`+ack[1]+`/ack"`) {
		t.Fatal("reopened hint not open again")
	}
}
