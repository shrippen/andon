// Package web wires HTTP routes to the services layer: routes never call
// repos, sources or drivers directly (see agent.md's layering rule).
package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"andon/internal/crypto"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/access"
	"andon/internal/services/auth"
	"andon/internal/settings"
)

const (
	CookieName = "dsh_session"
	CSRFHeader = "X-CSRF-Token"
	CSRFField  = "csrf"
	bearer     = "Bearer "
)

// Ctx is everything a page needs about the requester.
type Ctx struct {
	Who    *access.Principal
	CSRF   string
	Method enums.AuthMethod
	Locale enums.Locale
	Path   string // request path, marks the current menu entry
}

// ErrLoginRequired is turned into a redirect to /login by the caller.
var ErrLoginRequired = errors.New("web: login required")

// ErrTOTPPending means the session exists but still needs its second factor.
var ErrTOTPPending = errors.New("web: totp pending")

// ErrTOTPSetup means TOTP is forced for admins and this one hasn't set it up.
var ErrTOTPSetup = errors.New("web: totp setup required")

// totpSetupPaths stay reachable while a second factor (TOTP or passkey)
// must still be set up.
var totpSetupPaths = []string{"/me/security", "/me/passkeys", "/logout"}

// ErrCSRFFailed means the request's CSRF token was missing or wrong.
var ErrCSRFFailed = errors.New("web: csrf failed")

var safeMethods = map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true}

// clientIP is the request's client address. Behind a trusted reverse
// proxy (settings TrustedProxies) it is the last X-Forwarded-For entry
// not itself a trusted proxy; anyone else's header is ignored, it could
// be forged to dodge the login limit.
func (d Deps) clientIP(r *http.Request) string {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	addr := remote.Addr().Unmap()
	if !d.trusted(addr) {
		return addr.String()
	}

	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !d.trusted(hop.Unmap()) {
			return hop.Unmap().String()
		}
	}
	return addr.String()
}

func (d Deps) trusted(addr netip.Addr) bool {
	for _, p := range d.Settings.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Agent returns the request's User-Agent header.
func Agent(r *http.Request) string { return r.Header.Get("User-Agent") }

// Deps bundles what request handling needs from the rest of the app.
type Deps struct {
	DB       *sql.DB
	Settings settings.Settings
}

func (d Deps) session(r *http.Request) (*auth.SessionInfo, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, nil
	}
	return auth.Resolve(d.DB, d.Settings, cookie.Value)
}

// Context builds a Ctx for any request, authenticated or not.
func (d Deps) Context(r *http.Request) (Ctx, error) {
	ctx, _, err := d.resolve(r)
	return ctx, err
}

// resolve reads the session once and builds the Ctx from it.
func (d Deps) resolve(r *http.Request) (Ctx, *auth.SessionInfo, error) {
	info, err := d.session(r)
	if err != nil {
		return Ctx{}, nil, err
	}
	locale := i18n.Pick(r.Header.Get("Accept-Language"))
	if info == nil {
		return Ctx{Locale: locale, Path: r.URL.Path}, nil, nil
	}
	if info.Principal != nil {
		locale = info.Principal.Locale
	}
	return Ctx{Who: info.Principal, CSRF: info.CSRF, Method: info.Method, Locale: locale, Path: r.URL.Path}, info, nil
}

// authedHandler is a handler for signed-in users; it gets the checked Ctx.
type authedHandler func(w http.ResponseWriter, r *http.Request, ctx Ctx)

// authed runs h after Require: logged in, second factor done, CSRF valid
// on unsafe methods. Failures answer as handleAuthError does.
func (d Deps) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := d.Require(r)
		if err != nil {
			d.handleAuthError(w, r, err)
			return
		}
		h(w, r, ctx)
	}
}

// Require builds a Ctx and enforces that the caller is fully logged in
// (not pending 2FA) with a valid CSRF token on unsafe methods.
func (d Deps) Require(r *http.Request) (Ctx, error) {
	ctx, info, err := d.resolve(r)
	if err != nil {
		return Ctx{}, err
	}
	if info != nil && info.Pending2FA {
		return Ctx{}, ErrTOTPPending
	}
	if ctx.Who == nil {
		return Ctx{}, ErrLoginRequired
	}
	if err := d.checkCSRF(r, ctx.CSRF); err != nil {
		return Ctx{}, err
	}
	if err := d.checkTOTPSetup(r, ctx); err != nil {
		return Ctx{}, err
	}
	return ctx, nil
}

// checkTOTPSetup keeps an admin who must still set up TOTP on the
// security page.
func (d Deps) checkTOTPSetup(r *http.Request, ctx Ctx) error {
	for _, p := range totpSetupPaths {
		if strings.HasPrefix(r.URL.Path, p) {
			return nil
		}
	}
	required, err := auth.TOTPRequired(d.DB, ctx.Who, ctx.Method)
	if err != nil {
		return err
	}
	if required {
		return ErrTOTPSetup
	}
	return nil
}

func (d Deps) checkCSRF(r *http.Request, expected string) error {
	if safeMethods[r.Method] {
		return nil
	}
	sent := r.Header.Get(CSRFHeader)
	if sent == "" {
		sent = r.FormValue(CSRFField)
	}
	if sent == "" || expected == "" || !crypto.Same(sent, expected) {
		return ErrCSRFFailed
	}
	return nil
}

// Viewer builds a Ctx from the session, or from a bearer/query embed token
// on GET requests (iframes embedded in other dashboards).
func (d Deps) Viewer(r *http.Request) (Ctx, error) {
	ctx, err := d.Context(r)
	if err != nil {
		return Ctx{}, err
	}
	if ctx.Who != nil {
		return ctx, nil
	}
	who := d.apiPrincipal(r, enums.TokenEmbed, enums.TokenRead)
	if who == nil {
		return Ctx{}, ErrLoginRequired
	}
	return Ctx{Who: who, Locale: who.Locale}, nil
}

func (d Deps) tokenPrincipal(r *http.Request, scope enums.TokenScope) (*access.Principal, error) {
	header := r.Header.Get("Authorization")
	secret := r.URL.Query().Get(tokenParam)
	if strings.HasPrefix(header, bearer) {
		secret = strings.TrimPrefix(header, bearer)
	}
	if secret == "" {
		return nil, nil
	}
	return auth.PrincipalForToken(d.DB, secret, scope)
}

// setSession writes the session cookie for token.
func (d Deps) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: d.Settings.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

// clearSession removes the session cookie.
func (d Deps) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: d.Settings.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}
