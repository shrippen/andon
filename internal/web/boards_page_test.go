package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestBoardsPageOrdersNav: /boards lists every board with its actions;
// moving one up puts it first in the header, hiding takes it out of the
// header but not off the list.
func TestBoardsPageOrdersNav(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	mustGet(t, srv, client, "/") // creates "Start"
	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/boards/new"))[1]
	postForm(t, client, srv.URL+"/boards/new", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "name": {"Zweites"}})

	page := string(mustGet(t, srv, client, "/boards"))
	id := regexp.MustCompile(`id="board-(\d+)"[^>]*>\s*<td><a[^>]*>Zweites`).FindStringSubmatch(page)
	if id == nil || !strings.Contains(page, `?edit"`) {
		t.Fatalf("boards page lacks the board or its edit link:\n%s", page)
	}

	postForm(t, client, srv.URL+"/boards/"+id[1]+"/nav", url.Values{"csrf": {csrf}, "move": {"up"}})
	nav := regexp.MustCompile(`(?s)<nav class="app-links"[^>]*>(.*?)</nav>`).FindStringSubmatch(string(mustGet(t, srv, client, "/boards")))[1]
	if strings.Index(nav, "Zweites") < 0 || strings.Index(nav, "Zweites") > strings.Index(nav, "Start") {
		t.Fatalf("moved board not first in nav:\n%s", nav)
	}

	postForm(t, client, srv.URL+"/boards/"+id[1]+"/nav", url.Values{"csrf": {csrf}, "move": {"toggle"}})
	page = string(mustGet(t, srv, client, "/boards"))
	nav = regexp.MustCompile(`(?s)<nav class="app-links"[^>]*>(.*?)</nav>`).FindStringSubmatch(page)[1]
	if strings.Contains(nav, "Zweites") || !strings.Contains(page, "Zweites") {
		t.Fatalf("hidden board still in nav or gone from list:\n%s", nav)
	}
}
