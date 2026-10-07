package web_test

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// A Homelable record has a sync tab; "Sync now" draws the demo vault of
// the Gitea connection in the same space and lands there with the counts.
func TestHomelableSyncTab(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	resp, err := client.Get(srv.URL + "/connections/new?service=homelable")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(body)
	if space == nil {
		t.Fatalf("no space option:\n%s", body)
	}

	create := func(service, name, address string) string {
		t.Helper()
		resp, err := client.PostForm(srv.URL+"/connections", url.Values{
			"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space[1])}, "service": {service}, "name": {name},
			"url": {address}, "mode": {"shared"}, "secret": {"tok"}, "secret_a": {"andon"}, "secret_b": {"pw"}, "tls": {"verify"},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("create %s: %d", service, resp.StatusCode)
		}
		record, _, _ := strings.Cut(resp.Header.Get("Location"), "?")
		return record
	}
	create("gitea", "Gitea", "demo://gitea")
	record := create("homelable", "Homelable", "demo://homelable")

	page := getFollowingRedirect(t, srv, client, record+"?tab=sync")
	text, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(text), `id="tab-sync"`) || !strings.Contains(string(text), `action="`+record+`/sync"`) {
		t.Fatalf("no sync tab:\n%s", text)
	}

	resp, err = client.PostForm(srv.URL+record+"/sync", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "note=synced") {
		t.Fatalf("sync: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	page = getFollowingRedirect(t, srv, client, resp.Header.Get("Location"))
	text, _ = io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(text), `class="callout callout-ok"`) || !strings.Contains(string(text), `class="detail-facts"`) || strings.Contains(string(text), "homelable.") {
		t.Fatalf("no sync log:\n%s", text)
	}
}
