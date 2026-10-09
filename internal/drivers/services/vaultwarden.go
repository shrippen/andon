package services

// Vaultwarden admin API: POST /admin with the admin token sets the
// VW_ADMIN cookie; /admin/users then answers JSON. Vaultwarden allows
// three logins in five minutes (ADMIN_RATELIMIT_*), so the cookie is
// kept in memory until it expires (401, after 20 minutes by default):
//
//	Users ─► cookie known? ─yes─► GET admin/users ─401─┐
//	              │ no                                  │
//	              └──► POST admin ─► cookie ◄───────────┘ (once)

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"andon/internal/drivers/httpclient"
)

const vaultwardenCookie = "VW_ADMIN"

var (
	vaultwardenMu      sync.Mutex
	vaultwardenCookies = map[string]string{} // by address and token hash, never stored
)

type VaultwardenApi struct {
	URL    string
	Token  string // ADMIN_TOKEN (plain, as typed into the admin page)
	Verify bool
}

// Users lists the users, logging in first when no cookie is known and
// once more when it has expired.
func (a VaultwardenApi) Users(ctx context.Context) (any, error) {
	vaultwardenMu.Lock()
	cookie := vaultwardenCookies[a.cookieKey()]
	vaultwardenMu.Unlock()

	if cookie != "" {
		out, err := a.users(ctx, cookie)
		if err == nil || err.Error() != fmt.Sprintf("HTTP %d", http.StatusUnauthorized) {
			return out, err
		}
	}
	cookie, err := a.login(ctx)
	if err != nil {
		return nil, err
	}
	vaultwardenMu.Lock()
	vaultwardenCookies[a.cookieKey()] = cookie
	vaultwardenMu.Unlock()
	return a.users(ctx, cookie)
}

func (a VaultwardenApi) users(ctx context.Context, cookie string) (any, error) {
	return fetchJSON(ctx, joinURL(a.URL, "admin/users"), map[string]string{"Cookie": cookie, "Accept": "application/json"}, nil, httpclient.TLSOf(a.Verify))
}

// login trades the admin token for the session cookie.
func (a VaultwardenApi) login(ctx context.Context) (string, error) {
	form := url.Values{"token": {a.Token}}.Encode()
	resp, err := httpclient.Request(ctx, http.MethodPost, joinURL(a.URL, "admin"), httpclient.Options{
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:    []byte(form), SkipVerify: !a.Verify, NoRedirect: true,
	})
	if err != nil {
		return "", ApiError{err.Error()}
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ApiError{"login rate limit (ADMIN_RATELIMIT_MAX_BURST)"}
	}

	for _, c := range resp.Cookies() {
		if c.Name == vaultwardenCookie {
			return c.Name + "=" + c.Value, nil
		}
	}
	return "", ApiError{"login failed"}
}

// cookieKey keeps sessions apart by address and token, without holding
// the token itself.
func (a VaultwardenApi) cookieKey() string {
	sum := sha256.Sum256([]byte(a.Token))
	return strings.TrimRight(a.URL, "/") + "\x00" + hex.EncodeToString(sum[:])
}

// Version reads the server version ("" if the endpoint is missing).
func (a VaultwardenApi) Version(ctx context.Context) string {
	text, err := httpclient.GetText(ctx, joinURL(a.URL, "api/version"), httpclient.Options{SkipVerify: !a.Verify})
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(text), `"`)
}
