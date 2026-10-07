package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
)

// staleBoard is a board in edit mode whose version moved on after the
// page was loaded: what a second tab sees.
type staleBoard struct {
	url, id, section, stale, current string
}

func newStaleBoard(t *testing.T, post func(string, url.Values) *http.Response, get func(string) []byte, boardURL string) staleBoard {
	t.Helper()
	page := get(boardURL + "?edit")
	b := staleBoard{url: boardURL, id: boardIDFrom(boardURL)}
	b.section = string(regexp.MustCompile(`id="dsec-(\d+)"`).FindSubmatch(page)[1])
	b.stale = string(regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(page)[1])
	// The other tab adds a section: the version moves on.
	post("/boards/"+b.id+"/sections", url.Values{"version": {b.stale}, "title": {"Other tab"}})
	b.current = string(regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(get(boardURL + "?edit"))[1])
	if b.current == b.stale {
		t.Fatal("version did not move")
	}
	return b
}

// TestStaleFormsKeepInput: a form sent with an old board version (409)
// comes back with what was typed, the conflict named and the current
// version, so sending it again works.
func TestStaleFormsKeepInput(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path
	get := func(path string) []byte { return mustGet(t, srv, client, path) }
	post := func(path string, v url.Values) *http.Response {
		v.Set("csrf", csrfToken(t, srv, client))
		return postForm(t, client, srv.URL+path, v)
	}
	conflict := i18n.T("error.conflict_kept", enums.LocaleDE, nil)
	section := func(b staleBoard) map[string]string {
		return map[string]string{"HX-Request": "true", "HX-Target": "dsec-" + b.section}
	}

	t.Run("section edit", func(t *testing.T) {
		b := newStaleBoard(t, post, get, boardURL)
		form := url.Values{"csrf": {csrfToken(t, srv, client)}, "board_id": {b.id}, "version": {b.stale}, "title": {"Typed title"},
			"size": {"medium"}, "sort": {"manual"}, "area": {"main"}, "span": {"0"}, "rows": {"1"}, "mobile": {""},
			"icon": {"typed-icon"}, "was_area": {"main"}, "was_span": {"0"}}
		status, body := browse(t, client, http.MethodPost, srv.URL+"/sections/"+b.section+"/edit", form, section(b))
		if status != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(body), "<section") {
			t.Fatalf("expected the section, got %d:\n%.400s", status, body)
		}
		for _, want := range []string{conflict, `value="Typed title"`, `value="typed-icon"`, `name="version" value="` + b.current + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%q missing:\n%s", want, body)
			}
		}
	})

	t.Run("quick link", func(t *testing.T) {
		b := newStaleBoard(t, post, get, boardURL)
		form := url.Values{"csrf": {csrfToken(t, srv, client)}, "board_id": {b.id}, "version": {b.stale}, "url": {"https://typed.example"}}
		status, body := browse(t, client, http.MethodPost, srv.URL+"/sections/"+b.section+"/quick-link", form, section(b))
		if status != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(body), "<section") {
			t.Fatalf("expected the section, got %d:\n%.400s", status, body)
		}
		for _, want := range []string{conflict, `value="https://typed.example"`, `name="version" value="` + b.current + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%q missing:\n%s", want, body)
			}
		}
	})

	t.Run("section add", func(t *testing.T) {
		b := newStaleBoard(t, post, get, boardURL)
		form := url.Values{"csrf": {csrfToken(t, srv, client)}, "version": {b.stale}, "title": {"Typed section"}}
		status, body := browse(t, client, http.MethodPost, srv.URL+"/boards/"+b.id+"/sections", form, nil)
		if status != http.StatusConflict {
			t.Fatalf("expected 409, got %d", status)
		}
		for _, want := range []string{conflict, `value="Typed section"`, `name="version" value="` + b.current + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%q missing:\n%s", want, body)
			}
		}
		// Sent again as answered, it goes through.
		next, _ := strconv.Atoi(b.current)
		resp := post("/boards/"+b.id+"/sections", url.Values{"version": {b.current}, "title": {"Typed section"}})
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("again: %d", resp.StatusCode)
		}
		if v := regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(get(boardURL + "?edit")); string(v[1]) != strconv.Itoa(next+1) {
			t.Fatalf("version after: %s", v[1])
		}
	})
}
