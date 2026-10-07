package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/outbound"
	"andon/internal/services/mail"
	"andon/internal/settings"
)

// refusal is one form posted with a deliberate error: what must come
// back in the answer, and what never may.
type refusal struct {
	name    string
	prepare func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values)
	want    []string
	secrets []string
}

// asAdmin sets up and logs in the admin; the form gets its CSRF token.
func asAdmin(path string, form url.Values) func(*testing.T, *httptest.Server, *http.Client, string) (*http.Client, string, url.Values) {
	return func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values) {
		setupAdmin(t, srv, client, code)
		login(t, srv, client)
		form.Set("csrf", csrfToken(t, srv, client))
		return client, path, form
	}
}

// openRegistration lets anyone register (admin settings).
func openRegistration(t *testing.T, srv *httptest.Server, client *http.Client) {
	t.Helper()
	postForm(t, client, srv.URL+"/admin/settings/general", url.Values{"csrf": {csrfToken(t, srv, client)}, "registration": {"on"}})
}

// inviteLink invites address as the logged-in admin and returns the
// mailed link's path.
func inviteLink(t *testing.T, srv *httptest.Server, client *http.Client, address string) string {
	t.Helper()
	outbound.TakeOutbox()
	postForm(t, client, srv.URL+"/admin/invite", url.Values{"csrf": {csrfToken(t, srv, client)}, "email": {address}, "role": {"user"}, "locale": {"de"}})
	sent := outbound.TakeOutbox()
	if len(sent) != 1 {
		t.Fatalf("expected one invite mail, got %d", len(sent))
	}
	return linkRe.FindStringSubmatch(sent[0].Text)[1]
}

// TestFormsKeepInput: every form refused for a typing error comes back
// with what was typed, never with a password, token or code.
func TestFormsKeepInput(t *testing.T) {
	cases := []refusal{
		{name: "setup", prepare: func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values) {
			return client, "/setup", url.Values{"code": {"wrong-setup-code"}, "email": {"kept@x.de"}, "name": {"Kept Name"}, "password": {"Secret-Passw0rd-1"}}
		}, want: []string{`value="kept@x.de"`, `value="Kept Name"`}, secrets: []string{"wrong-setup-code", "Secret-Passw0rd-1"}},

		{name: "invite", prepare: func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values) {
			setupAdmin(t, srv, client, code)
			login(t, srv, client)
			path := inviteLink(t, srv, client, "guest@x.de")
			return freshClient(t), path, url.Values{"name": {"Invited Name"}, "password": {"short-pw"}, "locale": {"en"}}
		}, want: []string{`value="Invited Name"`, `value="en" selected`}, secrets: []string{"short-pw"}},

		{name: "register", prepare: func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values) {
			setupAdmin(t, srv, client, code)
			login(t, srv, client)
			openRegistration(t, srv, client)
			return freshClient(t), "/register", url.Values{"email": {"self@x.de"}, "name": {"Self Name"}, "password": {"short-pw"}, "locale": {"de"}}
		}, want: []string{`value="self@x.de"`, `value="Self Name"`}, secrets: []string{"short-pw"}},

		{name: "quiet hours", prepare: asAdmin("/me/notify/prefs", url.Values{"quiet_from": {"25:99"}, "quiet_to": {"07:00"}, "quiet_muted": {"on"}, "repeat_hours": {"5"}}),
			want: []string{`value="25:99"`, `value="07:00"`, `name="quiet_muted" checked`, `value="5"`}},

		{name: "notify channel", prepare: asAdmin("/me/notify/channels", url.Values{"name": {"Kept Channel"}, "url": {"no-scheme-user:pw-in-url"}, "level": {"30"}}),
			want: []string{`value="Kept Channel"`, `value="30" selected`}, secrets: []string{"pw-in-url"}},

		{name: "profile", prepare: asAdmin("/me/profile", url.Values{"name": {"Kept Profile"}, "locale": {"en"}, "theme_id": {"99999"}, "search_engine": {"https://kept.example/?q={query}"}}),
			want: []string{`value="Kept Profile"`, `value="https://kept.example/?q={query}"`}},

		{name: "password change", prepare: asAdmin("/me/security/password", url.Values{"current": {"wrong-current-pw"}, "new": {"New-Secret-Passw0rd"}}),
			secrets: []string{"wrong-current-pw", "New-Secret-Passw0rd"}},

		{name: "admin invite", prepare: asAdmin("/admin/invite", url.Values{"email": {"not-an-email"}, "role": {"admin"}, "locale": {"en"}}),
			want: []string{`value="not-an-email"`, `value="admin" selected`}},

		{name: "network", prepare: asAdmin("/admin/settings/network", url.Values{"mode": {"allowlist"}, "networks": {"not-a-cidr"}, "hosts": {"kept.example"}}),
			want: []string{">not-a-cidr</textarea>", ">kept.example</textarea>"}},

		{name: "new connection", prepare: asAdmin("/connections", url.Values{"service": {"kimai"}, "name": {"Kept Conn"}, "url": {"not a url"},
			"mode": {"shared"}, "tls": {"verify"}, "secret": {"tok-secret-value"}}),
			want: []string{`value="Kept Conn"`, `value="not a url"`}, secrets: []string{"tok-secret-value"}},

		{name: "verbund", prepare: func(t *testing.T, srv *httptest.Server, client *http.Client, code string) (*http.Client, string, url.Values) {
			setupAdmin(t, srv, client, code)
			login(t, srv, client)
			space := instanceSpace(t, srv, client)
			return client, "/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "name": {"Kept Verbund"}}
		}, want: []string{`value="Kept Verbund"`}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, client, code := newTestServer(t)
			poster, path, form := c.prepare(t, srv, client, code)
			status, body := browse(t, poster, http.MethodPost, srv.URL+path, form, nil)
			if status < http.StatusBadRequest {
				t.Fatalf("expected a refusal, got %d", status)
			}
			for _, w := range c.want {
				if !strings.Contains(body, w) {
					t.Errorf("%q missing after the refusal:\n%s", w, body)
				}
			}
			for _, s := range c.secrets {
				if strings.Contains(body, s) {
					t.Errorf("secret %q came back", s)
				}
			}
		})
	}
}

// flashText is a success message as the German pages show it.
func flashText(key string) string { return i18n.T(key, enums.LocaleDE, nil) }

// TestFlashAfterRedirect: setup, a password reset and a password change
// say once that they worked, on the page they lead to.
func TestFlashAfterRedirect(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)

	_, body := browse(t, client, http.MethodGet, srv.URL+"/login", nil, nil)
	if !strings.Contains(body, flashText("flash.setup_done")) {
		t.Fatalf("no success message after setup:\n%s", body)
	}
	if _, again := browse(t, client, http.MethodGet, srv.URL+"/login", nil, nil); strings.Contains(again, flashText("flash.setup_done")) {
		t.Fatal("success message shown twice")
	}

	login(t, srv, client)
	resp := postForm(t, client, srv.URL+"/me/security/password", url.Values{"csrf": {csrfToken(t, srv, client)},
		"current": {"s3cret-password-long"}, "new": {"changed-password-long"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("password change: %d", resp.StatusCode)
	}
	if _, body := browse(t, client, http.MethodGet, srv.URL+resp.Header.Get("Location"), nil, nil); !strings.Contains(body, flashText("flash.password_changed")) {
		t.Fatalf("no success message after the password change:\n%s", body)
	}

	guest := freshClient(t)
	outbound.TakeOutbox()
	postForm(t, guest, srv.URL+"/reset", url.Values{"email": {"admin@x.de"}})
	sent := outbound.TakeOutbox()
	if len(sent) != 1 {
		t.Fatalf("expected a reset mail, got %d", len(sent))
	}
	resp = postForm(t, guest, srv.URL+linkRe.FindStringSubmatch(sent[0].Text)[1], url.Values{"password": {"reset-password-long"}})
	if _, body := browse(t, guest, http.MethodGet, srv.URL+resp.Header.Get("Location"), nil, nil); !strings.Contains(body, flashText("flash.password_reset")) {
		t.Fatalf("no success message after the reset:\n%s", body)
	}
}

// withoutMail runs the rest of a test as an instance without SMTP.
func withoutMail(t *testing.T) {
	t.Helper()
	mail.Init(settings.Settings{BaseURL: "http://dash.test"})
	t.Cleanup(func() { mail.Init(settings.Settings{BaseURL: "http://dash.test", Testing: true}) })
}

// TestForgotWithoutMail: without SMTP the login page offers to ask an
// admin, and the reset page explains the admin's reset link instead of
// promising a mail.
func TestForgotWithoutMail(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	guest := freshClient(t)

	_, body := browse(t, guest, http.MethodGet, srv.URL+"/login", nil, nil)
	if !strings.Contains(body, flashText("login.forgot")) {
		t.Fatalf("with mail the login page offers the reset by mail:\n%s", body)
	}

	withoutMail(t)
	_, body = browse(t, guest, http.MethodGet, srv.URL+"/login", nil, nil)
	if strings.Contains(body, flashText("login.forgot")) || !strings.Contains(body, flashText("login.ask_admin")) {
		t.Fatalf("without mail the login page must send to an admin:\n%s", body)
	}
	_, body = browse(t, guest, http.MethodGet, srv.URL+"/reset", nil, nil)
	if strings.Contains(body, `action="/reset"`) || !strings.Contains(body, flashText("reset.ask_admin")) {
		t.Fatalf("without mail the reset page must explain the admin's link:\n%s", body)
	}
	outbound.TakeOutbox()
	status, body := browse(t, guest, http.MethodPost, srv.URL+"/reset", url.Values{"email": {"admin@x.de"}}, nil)
	if status != http.StatusOK || !strings.Contains(body, flashText("reset.ask_admin")) || len(outbound.TakeOutbox()) != 0 {
		t.Fatalf("a reset request without mail: %d\n%s", status, body)
	}
}

// registerAnswer posts a registration from a fresh browser: status,
// target and the cookies it sets.
func registerAnswer(t *testing.T, srv *httptest.Server, address string) string {
	t.Helper()
	resp := postForm(t, freshClient(t), srv.URL+"/register", url.Values{"email": {address}, "name": {"Someone"},
		"password": {"register-password-1"}, "locale": {"de"}})
	var cookies []string
	for _, c := range resp.Cookies() {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	return fmt.Sprint(resp.StatusCode, " ", resp.Header.Get("Location"), " ", cookies)
}

// TestRegisterSameAnswer: registration answers the same for a new and a
// taken address, with and without SMTP; with SMTP the owner of a taken
// address learns by mail that they already have an account.
func TestRegisterSameAnswer(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	openRegistration(t, srv, client)
	outbound.TakeOutbox()

	fresh, taken := registerAnswer(t, srv, "new@x.de"), registerAnswer(t, srv, "admin@x.de")
	if fresh != taken {
		t.Fatalf("registration tells a taken address apart:\nnew:   %s\ntaken: %s", fresh, taken)
	}
	sent := outbound.TakeOutbox()
	if len(sent) != 1 || sent[0].To != "admin@x.de" || !strings.Contains(sent[0].Subject, flashText("mail.exists.subject")) ||
		!strings.Contains(sent[0].Text, "http://dash.test/reset") {
		t.Fatalf("expected one mail to the taken address with a reset link, got %+v", sent)
	}
	resp := postForm(t, freshClient(t), srv.URL+"/login", url.Values{"email": {"new@x.de"}, "password": {"register-password-1"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("the new account cannot log in: %d", resp.StatusCode)
	}
	login(t, srv, freshClient(t)) // the taken account keeps its password

	withoutMail(t)
	fresh, taken = registerAnswer(t, srv, "newer@x.de"), registerAnswer(t, srv, "admin@x.de")
	if fresh != taken {
		t.Fatalf("without mail registration tells a taken address apart:\nnew:   %s\ntaken: %s", fresh, taken)
	}
}
