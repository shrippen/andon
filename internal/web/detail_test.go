package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDetailRoute: a placed link with status check opens its dialog under
// /details/{id} with the shared head; an unknown tile has none.
func TestDetailRoute(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer target.Close()

	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path
	add := addLinkRe.FindSubmatch(mustGet(t, srv, client, boardURL+"?edit"))
	postForm(t, client, srv.URL+"/sections/"+string(add[1])+"/quick-link", url.Values{"csrf": {csrf},
		"version": {string(add[2])}, "board_id": {boardIDFrom(boardURL)}, "url": {target.URL}})
	placement := string(placementRe.FindSubmatch(mustGet(t, srv, client, boardURL+"?edit"))[1])

	body := string(mustGet(t, srv, client, "/details/"+placement))
	for _, want := range []string{`class="detail-head"`, `data-detail-refresh="/widget-fragments/` + placement + `?refresh"`, `class="detail-body"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}

	r, err := client.Get(srv.URL + "/details/999999")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown tile: %d", r.StatusCode)
	}
}
