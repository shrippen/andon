package web_test

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// editorParam: the page's parameter that opens the editor (routes_editor.go).
const editorParam = "editor"

// noRedirects is client without following redirects.
func noRedirects(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// TestEditorOnlyInDialog: the editor and the gallery exist only as the
// dialog. Their plain addresses (a link opened in a new tab) lead to the
// page the dialog belongs to, which opens it (?editor=).
func TestEditorOnlyInDialog(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "type": {"note"}, "title": {"My Note"}, "cfg.text": {"hi"}})
	boardURL, section, version, widget := placeTarget(t, srv, client, "My Note")
	board := boardIDFrom(boardURL)

	cases := []struct{ path, back, editor string }{
		{"/widgets/" + widget + "/edit", "/widgets", "/widgets/" + widget + "/edit?dialog"},
		{"/widgets/" + widget + "/edit?board=" + board, "/boards/" + board + "?edit", "/widgets/" + widget + "/edit?board=" + board + "&dialog"},
		{"/widgets/new?type=note&space=" + string(space), "/widgets", "/widgets/new?type=note&space=" + string(space) + "&dialog"},
		{"/widgets/new?space=" + string(space) + "&section=" + section + "&board=" + board + "&version=" + version,
			"/boards/" + board + "?edit", "/widgets/new?space=" + string(space) + "&section=" + section + "&board=" + board + "&version=" + version + "&dialog"},
	}
	for _, c := range cases {
		resp, err := noRedirects(client).Get(srv.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if resp.StatusCode != http.StatusSeeOther || loc == nil {
			t.Fatalf("%s: %d, want a redirect", c.path, resp.StatusCode)
		}
		got := loc.Query().Get(editorParam)
		loc.RawQuery = strings.TrimSuffix(strings.Replace(loc.RawQuery, editorParam+"="+url.QueryEscape(got), "", 1), "&")
		if back := strings.TrimSuffix(loc.String(), "?"); back != c.back || got != c.editor {
			t.Fatalf("%s → %s opens %q, want %s opening %q", c.path, back, got, c.back, c.editor)
		}
	}

	gallery := string(mustGet(t, srv, client, "/widgets/new?dialog&space="+string(space)))
	if !strings.Contains(gallery, `class="detail-head"`) || strings.Contains(gallery, "<html") {
		t.Fatalf("gallery is no dialog:\n%s", gallery)
	}
}

// TestEditorErrorsStayInDialog: a refused save answers the dialog with
// the catalog's message, not the editor page with the raw error.
func TestEditorErrorsStayInDialog(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "type": {"note"}, "title": {"My Note"}, "cfg.text": {"hi"}})
	_, _, _, widget := placeTarget(t, srv, client, "My Note")

	stale := url.Values{"csrf": {csrf}, "type": {"note"}, "title": {"Neu"}, "cfg.text": {"x"}, "widget_version": {"0"}, "space_id": {string(space)}}
	empty := url.Values{"csrf": {csrf}, "type": {"rss"}, "space_id": {string(space)}, "cfg.__fields": {"1"}, "cfg.url": {""}}
	cases := []struct {
		path   string
		values url.Values
		want   string
	}{
		{"/widgets/" + widget + "/edit", stale, "Inzwischen hat jemand anderes gespeichert"},
		{"/widgets", empty, "Bitte alle Pflichtfelder ausfüllen"},
	}
	for _, c := range cases {
		resp, err := client.PostForm(srv.URL+c.path, c.values)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		page := string(body)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, `class="detail-head"`) || strings.Contains(page, "<html") {
			t.Fatalf("%s: %d, want the dialog:\n%s", c.path, resp.StatusCode, page)
		}
		if !strings.Contains(page, c.want) {
			t.Fatalf("%s: misses %q:\n%s", c.path, c.want, page)
		}
	}
}

// TestEditorDeleteReturnsToBoard: deleting from the board's dialog goes
// back to the board, not to the library.
func TestEditorDeleteReturnsToBoard(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "type": {"note"}, "title": {"My Note"}, "cfg.text": {"hi"}})
	boardURL, _, _, widget := placeTarget(t, srv, client, "My Note")
	board := boardIDFrom(boardURL)

	dialog := string(mustGet(t, srv, client, "/widgets/"+widget+"/edit?board="+board+"&dialog"))
	action := regexp.MustCompile(`action="(/widgets/` + widget + `/delete[^"]*)"`).FindStringSubmatch(dialog)
	if action == nil {
		t.Fatalf("dialog has no delete form:\n%s", dialog)
	}
	resp := postForm(t, noRedirects(client), srv.URL+strings.ReplaceAll(action[1], "&amp;", "&"), url.Values{"csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/boards/"+board+"?edit" {
		t.Fatalf("delete goes to %q, want the board", loc)
	}
}
