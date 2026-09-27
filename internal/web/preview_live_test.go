package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestPreviewShowsWholeTile: the live preview answers with the whole
// card, so a typed title and frame options (accent, header) show at once;
// before, only the body changed and the heading kept the old title.
func TestPreviewShowsWholeTile(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	form := string(mustGet(t, srv, client, "/widgets/new?type=note"))
	for _, want := range []string{`name="cfg.frame_accent"`, `name="cfg.frame_header"`, `name="cfg.frame_round"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("form lacks %s", want)
		}
	}
	values := url.Values{"csrf": {csrf}, "type": {"note"}, "title": {"Neuer Titel"}, "cfg.text": {"Hallo"}, "cfg.frame_accent": {"green"}}
	if space := regexp.MustCompile(`name="space_id" value="(\d+)"`).FindStringSubmatch(form); space != nil {
		values.Set("space_id", space[1])
	}
	preview := func() string {
		resp, err := client.PostForm(srv.URL+"/widget-preview", values)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return readAll(t, resp)
	}
	body := preview()
	if !strings.Contains(body, `<article class="card" data-accent="green">`) || !strings.Contains(body, "Neuer Titel") || !strings.Contains(body, "Hallo") {
		t.Fatalf("preview is not the whole tile:\n%s", body)
	}
	values.Set("cfg.frame_header", "off")
	if body := preview(); strings.Contains(body, "Neuer Titel") {
		t.Fatalf("header off still shows the title:\n%s", body)
	}
}
