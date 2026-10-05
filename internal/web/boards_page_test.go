package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestBoardsPageOrdersNav: /boards shows every board as a card with its
// actions; moving one up puts it first in the header, a dragged order is
// saved, hiding takes it out of the header but not off the list.
func TestBoardsPageOrdersNav(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	mustGet(t, srv, client, "/") // creates "Start"
	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/boards/new"))[1]
	postForm(t, client, srv.URL+"/boards/new", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "name": {"Zweites"}})

	page := string(mustGet(t, srv, client, "/boards"))
	id := regexp.MustCompile(`<div class="board-name"><a href="/boards/(\d+)">Zweites</a>`).FindStringSubmatch(page)
	if id == nil || !strings.Contains(page, `?edit"`) {
		t.Fatalf("boards page lacks the board or its edit link:\n%s", page)
	}

	postForm(t, client, srv.URL+"/boards/"+id[1]+"/nav", url.Values{"csrf": {csrf}, "move": {"up"}})
	nav := regexp.MustCompile(`(?s)<nav class="app-links"[^>]*>(.*?)</nav>`).FindStringSubmatch(string(mustGet(t, srv, client, "/boards")))[1]
	if !strings.Contains(nav, "Zweites") || strings.Index(nav, "Zweites") > strings.Index(nav, "Start") {
		t.Fatalf("moved board not first in nav:\n%s", nav)
	}

	start := regexp.MustCompile(`<div class="board-name"><a href="/boards/(\d+)">Start</a>`).FindStringSubmatch(page)[1]
	resp := postForm(t, client, srv.URL+"/boards/order", url.Values{"csrf": {csrf}, "id": {start, id[1]}})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("order saved with %d", resp.StatusCode)
	}
	nav = regexp.MustCompile(`(?s)<nav class="app-links"[^>]*>(.*?)</nav>`).FindStringSubmatch(string(mustGet(t, srv, client, "/boards")))[1]
	if strings.Index(nav, "Zweites") < strings.Index(nav, "Start") {
		t.Fatalf("dragged order not in nav:\n%s", nav)
	}

	postForm(t, client, srv.URL+"/boards/"+id[1]+"/nav", url.Values{"csrf": {csrf}, "move": {"toggle"}})
	page = string(mustGet(t, srv, client, "/boards"))
	nav = regexp.MustCompile(`(?s)<nav class="app-links"[^>]*>(.*?)</nav>`).FindStringSubmatch(page)[1]
	if strings.Contains(nav, "Zweites") || !strings.Contains(page, `is-off" id="board-`+id[1]+`"`) {
		t.Fatalf("hidden board still in nav or gone from list:\n%s", nav)
	}
}
