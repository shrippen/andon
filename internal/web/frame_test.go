package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTileFrameOnBoard: a hints tile asked to show only when needed marks
// itself calm on a fresh instance (no hints), and its frame reaches the
// board: accent and a small header.
func TestTileFrameOnBoard(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))
	resp := postForm(t, client, srv.URL+"/widgets", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space[1])}, "type": {"hints"}, "title": {"Lage"},
		"cfg.frame_only_issues": {"on"}, "cfg.frame_accent": {"red"}, "cfg.frame_header": {"small"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create widget: %d", resp.StatusCode)
	}
	boardURL, sectionID, version, widgetID := placeTarget(t, srv, client, "Lage")
	postForm(t, client, srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections/"+sectionID+"/place", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "widget_id": {widgetID}, "version": {version},
	})
	board := string(mustGet(t, srv, client, boardURL))
	if !strings.Contains(board, `data-accent="red"`) || !strings.Contains(board, `data-size="small"`) {
		t.Fatalf("frame missing on the board:\n%s", board)
	}
	placement := string(regexp.MustCompile(`/widget-fragments/(\d+)"`).FindStringSubmatch(board)[1])
	if frag := string(mustGet(t, srv, client, "/widget-fragments/"+placement)); !strings.Contains(frag, `class="calm-mark"`) {
		t.Fatalf("calm tile not marked:\n%s", frag)
	}
}
