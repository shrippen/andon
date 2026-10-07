package web_test

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// TestTOTPThrottleSaysSo: after too many wrong codes even the right one
// is refused for a while; the page must say "too many attempts", not
// "invalid code", or the user hunts a clock drift.
func TestTOTPThrottleSaysSo(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	resp, err := client.PostForm(srv.URL+"/me/security/totp/begin", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	secret := string(regexp.MustCompile(`<p class="mono">([A-Z2-7]+)</p>`).FindSubmatch(body)[1])
	now, _ := totp.GenerateCode(secret, time.Now())
	if r, _ := client.PostForm(srv.URL+"/me/security/totp/confirm", url.Values{"csrf": {csrfToken(t, srv, client)}, "code": {now}}); r != nil {
		r.Body.Close()
	}

	other := freshClient(t)
	if r, _ := other.PostForm(srv.URL+"/login", url.Values{"email": {"admin@x.de"}, "password": {"s3cret-password-long"}}); r != nil {
		r.Body.Close()
	}
	for range 5 {
		if r, _ := other.PostForm(srv.URL+"/login/totp", url.Values{"code": {"000000"}}); r != nil {
			r.Body.Close()
		}
	}
	right, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	r, err := other.PostForm(srv.URL+"/login/totp", url.Values{"code": {right}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("throttled code: %d\n%s", r.StatusCode, page)
	}
}

// Setting up TOTP shows a QR code to scan; a mistyped confirmation code
// keeps the same secret on screen, so the app need not be scanned again.
func TestTOTPSetupQRAndRetry(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	resp, err := client.PostForm(srv.URL+"/me/security/totp/begin", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !regexp.MustCompile(`<img[^>]+src="data:image/png;base64,`).Match(body) {
		t.Fatal("no QR code")
	}
	secret := regexp.MustCompile(`<p class="mono">([A-Z2-7]+)</p>`).FindSubmatch(body)[1]

	resp, err = client.PostForm(srv.URL+"/me/security/totp/confirm", url.Values{"csrf": {csrfToken(t, srv, client)}, "code": {"000000"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	again := regexp.MustCompile(`<p class="mono">([A-Z2-7]+)</p>`).FindSubmatch(body)
	if again == nil || string(again[1]) != string(secret) {
		t.Fatal("wrong code dropped the setup")
	}
}
