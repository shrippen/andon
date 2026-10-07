package web_test

import (
	"net/http"
	"testing"
)

// TestPostPagesReload: pages rendered as the answer to a form keep their
// address; reloading them (a GET) leads to the page of the form, not to
// a bare "Method Not Allowed".
func TestPostPagesReload(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for path, want := range map[string]string{"/admin/invite": "/admin/users", "/me/security/totp/confirm": "/me/security", "/me/security/totp/begin": "/me/security"} {
		resp, err := noFollow.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("GET %s: %d → %q", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}
