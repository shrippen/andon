package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// pageMarker is in every page with the app frame (header with the
// signed-in navigation), never in a bare text answer.
const pageMarker = `class="app-links nav-wide"`

// browse sends a request as a browser page load (or with extra headers,
// e.g. htmx) and returns status and body.
func browse(t *testing.T, client *http.Client, method, target string, form url.Values, header map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestErrorPagesFramed: a failed page request of a browser keeps its
// status but shows the app frame, a message and a way back; htmx and
// API answers stay bare.
func TestErrorPagesFramed(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	board := resp.Request.URL.Path

	framed := []struct {
		name   string
		method string
		path   string
		form   url.Values
		want   int
	}{
		{"unknown path", http.MethodGet, "/no-such-page", nil, http.StatusNotFound},
		{"missing theme", http.MethodGet, "/themes/99999", nil, http.StatusNotFound},
		{"wrong method", http.MethodGet, "/me/locale", nil, http.StatusMethodNotAllowed},
		{"csrf", http.MethodPost, "/me/locale", url.Values{"locale": {"en"}}, http.StatusForbidden},
		{"stale version", http.MethodPost, board + "/settings",
			url.Values{"csrf": {csrfToken(t, srv, client)}, "name": {"Typed name"}, "version": {"999"}}, http.StatusConflict},
	}
	for _, c := range framed {
		status, body := browse(t, client, c.method, srv.URL+c.path, c.form, nil)
		if status != c.want {
			t.Errorf("%s: %d, want %d", c.name, status, c.want)
		}
		if !strings.Contains(body, pageMarker) || !strings.Contains(body, `data-back`) || strings.Contains(body, "error_page.") {
			t.Errorf("%s: no app frame or way back:\n%s", c.name, body)
		}
	}

	// htmx swaps the answer into the page: it stays bare text.
	status, body := browse(t, client, http.MethodGet, srv.URL+"/no-such-page", nil, map[string]string{"HX-Request": "true"})
	if status != http.StatusNotFound || strings.Contains(body, pageMarker) {
		t.Errorf("htmx: %d framed:\n%s", status, body)
	}

	// API clients read JSON or text, not a page.
	status, body = browse(t, client, http.MethodGet, srv.URL+"/api/no-such", nil, map[string]string{"Accept": "application/json"})
	if status != http.StatusNotFound || strings.Contains(body, pageMarker) {
		t.Errorf("api: %d framed:\n%s", status, body)
	}
}
