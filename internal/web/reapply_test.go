package web_test

import (
	"net/http"
	"testing"
)

// TestReapplyUnknownUser: an unknown user id is a 404, not a server
// error.
func TestReapplyUnknownUser(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	resp, err := client.Get(srv.URL + "/admin/users/9999/reapply")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown user: %d", resp.StatusCode)
	}
}
