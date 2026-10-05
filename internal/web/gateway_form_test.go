package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestGatewayFormRouterKind: the gateway form asks for the router, offers
// OpenWrt with user and password labels, and stores the choice.
func TestGatewayFormRouterKind(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	form := string(mustGet(t, srv, client, "/connections/new?service=gateway"))
	for _, want := range []string{`<select class="select" id="opt_kind" name="opt_kind">`, `<option value="openwrt">OpenWrt</option>`,
		`<span class="only-openwrt">Benutzername</span>`, `<span class="only-openwrt">Passwort</span>`} {
		if !strings.Contains(form, want) {
			t.Fatalf("form lacks %q:\n%s", want, form)
		}
	}
	space := regexp.MustCompile(`<option value="(\d+)"`).FindStringSubmatch(form)[1]
	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"gateway"}, "space_id": {space},
		"name": {"Router"}, "url": {"https://192.0.2.1"}, "mode": {"shared"}, "opt_kind": {"openwrt"},
		"secret_a": {"root"}, "secret_b": {"pw"}, "tls": {"verify"}})
	id := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))
	if id == nil {
		t.Fatalf("create: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if edit := string(mustGet(t, srv, client, "/connections/"+id[1]+"/edit")); !strings.Contains(edit, `<option value="openwrt" selected>`) {
		t.Fatal("router kind not stored")
	}
}
