package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestKDEStoreFormFields: the KDE Store form asks for user and entries,
// neither required, and keeps pasted store links as entry ids.
func TestKDEStoreFormFields(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	form := string(mustGet(t, srv, client, "/connections/new?service=kdestore"))
	for _, want := range []string{`name="opt_user" value=""`, `name="opt_ids" value=""`} {
		if !strings.Contains(form, want) {
			t.Fatalf("form lacks %q", want)
		}
	}
	if regexp.MustCompile(`name="opt_(user|ids)"[^>]*required`).MatchString(form) {
		t.Fatal("user and ids must be optional")
	}

	space := regexp.MustCompile(`<option value="(\d+)"`).FindStringSubmatch(form)[1]
	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"kdestore"}, "space_id": {space},
		"name": {"Store"}, "url": {"https://api.kde-look.org"}, "opt_user": {""},
		"opt_ids": {"https://store.kde.org/p/2368175, 2368948"}, "tls": {"verify"}})
	id := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))
	if id == nil {
		t.Fatalf("create: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if edit := string(mustGet(t, srv, client, "/connections/"+id[1]+"/edit")); !strings.Contains(edit, `name="opt_ids" value="2368175, 2368948"`) {
		t.Fatal("entry ids not stored as ids")
	}
}
