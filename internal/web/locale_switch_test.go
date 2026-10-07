package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// getLang GETs path with a German browser and returns the page.
func getLang(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept-Language", "de-DE,de;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// Logged out, the language switch on the login page keeps its choice in
// a cookie, over the browser's language.
func TestLocaleSwitchLoggedOut(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)

	page := getLang(t, client, srv.URL+"/login")
	if !strings.Contains(page, `lang="de"`) || !strings.Contains(page, `action="/locale"`) {
		t.Fatalf("login page without German default or language switch: %s", page)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/locale", strings.NewReader("locale=en"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", srv.URL+"/login")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("switch: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if page := getLang(t, client, srv.URL+"/login"); !strings.Contains(page, `lang="en"`) || !strings.Contains(page, `aria-pressed="true">EN`) {
		t.Fatalf("login page not in English after the switch: %s", page)
	}

	resp = postForm(t, client, srv.URL+"/locale", url.Values{"locale": {"xx"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown locale: %d", resp.StatusCode)
	}
}

// Logged in, the footer's switch stores the language in the profile.
func TestLocaleSwitchLoggedIn(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	page := getLang(t, client, srv.URL+"/hints")
	if !strings.Contains(page, `<footer`) || !strings.Contains(page, `action="/locale"`) {
		t.Fatalf("app page without footer switch: %s", page)
	}

	resp := postForm(t, client, srv.URL+"/locale", url.Values{"locale": {"en"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("switch without CSRF: %d", resp.StatusCode)
	}
	resp = postForm(t, client, srv.URL+"/locale", url.Values{"csrf": {csrf}, "locale": {"en"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("switch: %d", resp.StatusCode)
	}

	// The profile holds it: a fresh client (no language cookie) reads English.
	other := freshClient(t)
	resp = postForm(t, other, srv.URL+"/login", url.Values{"email": {"admin@x.de"}, "password": {"s3cret-password-long"}})
	resp.Body.Close()
	if page := getLang(t, other, srv.URL+"/hints"); !strings.Contains(page, `lang="en"`) {
		t.Fatalf("stored locale not used: %s", page[:200])
	}
}
