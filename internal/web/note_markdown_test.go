package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestNoteMarkdown: a note in Markdown shows bold and links, and HTML in
// it stays text.
func TestNoteMarkdown(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))
	postForm(t, client, srv.URL+"/widgets", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space[1])}, "type": {"note"}, "title": {"Merkzettel"},
		"cfg.text": {"**Backup** prüfen <script>alert(1)</script>\n- [Doku](https://example.org/doc)"}, "cfg.markdown": {"on"}, "cfg.color": {"blue"},
	})
	boardURL, sectionID, version, widgetID := placeTarget(t, srv, client, "Merkzettel")
	postForm(t, client, srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections/"+sectionID+"/place", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "widget_id": {widgetID}, "version": {version},
	})
	board := string(mustGet(t, srv, client, boardURL))
	for _, want := range []string{"<strong>Backup</strong>", `<a href="https://example.org/doc"`, "&lt;script&gt;", `data-color="blue"`} {
		if !strings.Contains(board, want) {
			t.Fatalf("%q missing:\n%s", want, board)
		}
	}
	if strings.Contains(board, "<script>alert(1)") {
		t.Fatal("raw HTML from the note reached the page")
	}
}
