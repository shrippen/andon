package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestPGBackWebHook: the edit page shows a signed URL; events sent to it
// show up in the connection test; a wrong signature is rejected.
func TestPGBackWebHook(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))
	resp := postForm(t, client, srv.URL+"/connections", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space[1])}, "service": {"pgbackweb"}, "name": {"PG"},
		"url": {"https://pgback.lan"}, "mode": {"shared"}, "tls": {"verify"},
	})
	edit := resp.Header.Get("Location")
	hook := regexp.MustCompile(`http://dash\.test(/hooks/\d+/[A-Za-z0-9_-]+)`).FindStringSubmatch(string(mustGet(t, srv, client, edit)))
	if hook == nil {
		t.Fatal("no hook URL on the edit page")
	}

	post := func(path, body string) int {
		r, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if got := post(hook[1], `{"event":"execution_success","name":"kimai"}`); got != http.StatusNoContent {
		t.Fatalf("hook: %d", got)
	}
	if got := post(hook[1]+"x", `{"event":"execution_failed","name":"kimai"}`); got != http.StatusNotFound {
		t.Fatalf("bad signature: %d", got)
	}

	id := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(edit)[1]
	test := readAll(t, postForm2(t, client, srv.URL+"/connections/"+id+"/test", url.Values{"csrf": {csrfToken(t, srv, client)}}))
	if !strings.Contains(test, "✓") {
		t.Fatalf("connection test failed:\n%s", test)
	}
}

// TestHanseiStateHook: Hansei pushes its whole state to the same signed
// URL; the connection test reads it back.
func TestHanseiStateHook(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))
	resp := postForm(t, client, srv.URL+"/connections", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space[1])}, "service": {"hansei"}, "name": {"Hansei"},
		"url": {"https://hansei.local"}, "mode": {"shared"}, "tls": {"verify"},
	})
	edit := resp.Header.Get("Location")
	hook := regexp.MustCompile(`http://dash\.test(/hooks/\d+/[A-Za-z0-9_-]+)`).FindStringSubmatch(string(mustGet(t, srv, client, edit)))
	if hook == nil {
		t.Fatal("no hook URL on the edit page")
	}

	r, err := http.Post(srv.URL+hook[1], "application/json", strings.NewReader(`{"state":{"review":4,"feedback":1,"done":2,"conformity":0.5,"claimed":["docs.missing:regis/kometa"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("hook: %d", r.StatusCode)
	}

	id := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(edit)[1]
	test := readAll(t, postForm2(t, client, srv.URL+"/connections/"+id+"/test", url.Values{"csrf": {csrfToken(t, srv, client)}}))
	if !strings.Contains(test, "✓") || !strings.Contains(test, "4") {
		t.Fatalf("connection test:\n%s", test)
	}
}

// postForm2 posts and keeps the body open.
func postForm2(t *testing.T, client *http.Client, target string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(target, form)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestHookWithoutURL: a webhook service needs no address of its own;
// its record opens with the URL to enter there, ready to copy.
func TestHookWithoutURL(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	form := string(mustGet(t, srv, client, "/connections/new?service=pgbackweb"))
	if regexp.MustCompile(`id="url"[^>]*required`).MatchString(form) {
		t.Fatal("address required for a webhook service")
	}

	space := regexp.MustCompile(`<option value="(\d+)">`).FindStringSubmatch(form)
	resp := postForm(t, client, srv.URL+"/connections", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {space[1]}, "service": {"pgbackweb"}, "name": {"PG"},
		"url": {""}, "mode": {"shared"}, "tls": {"verify"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: %d", resp.StatusCode)
	}

	// The overview tab, not a hidden one, carries the URL with a copy button.
	page := string(mustGet(t, srv, client, resp.Header.Get("Location")))
	overview := page[strings.Index(page, `id="tab-overview"`):strings.Index(page, `id="tab-access"`)]
	if !regexp.MustCompile(`class="cmd-box">http://dash\.test/hooks/\d+/`).MatchString(overview) || !strings.Contains(overview, "copy-btn") {
		t.Fatal("no hook URL to copy on the overview")
	}
}
