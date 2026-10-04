package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestLinkPreviewShowsIcon: the editor's preview of a link tile shows its
// icon (emoji or image), not only the initials.
func TestLinkPreviewShowsIcon(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := string(regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1])

	preview := func(icon string) string {
		t.Helper()
		form := url.Values{"csrf": {csrf}, "type": {"link"}, "space_id": {space}, "title": {"Nextcloud"},
			"cfg.url": {"https://cloud.example"}, "cfg.icon": {icon}, "cfg.status": {"off"}}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/widget-preview", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return readAll(t, res)
	}
	if got := preview("🧪"); !strings.Contains(got, `<span class="emoji" aria-hidden="true">🧪</span>`) {
		t.Fatalf("emoji preview:\n%s", got)
	}
	// An uploaded icon (a downloaded one shows the same once cached).
	if got := preview(uploadIcon(t, client, srv.URL, csrf)); !strings.Contains(got, `<img src="/icons/`) {
		t.Fatalf("icon preview:\n%s", got)
	}
}
