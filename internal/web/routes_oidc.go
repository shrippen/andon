package web

import (
	"net/http"

	"andon/internal/services/auth"
	"andon/internal/services/oidc"
	"andon/internal/services/util"
)

const profileHome = "/me/security"

// RegisterOIDCRoutes wires single sign-on: start, callback, and linking an
// existing account from the security page.
func (d Deps) RegisterOIDCRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/oidc/login", d.handleOIDCLogin)
	mux.HandleFunc("GET "+oidc.CallbackPath, d.handleOIDCCallback)
	mux.HandleFunc("POST /me/oidc/link", d.authed(d.handleOIDCLink))
}

// oidcCookie holds the state of a started flow in its browser, so the
// callback cannot be finished elsewhere (login CSRF, link hijack).
const (
	oidcCookie    = "andon_oidc"
	oidcCookieAge = 10 * 60 // seconds, as oidc's state lifetime
)

// safeNext keeps ?next= to local paths: no open redirect.
func safeNext(target string) string {
	return util.LocalPath(target)
}

func (d Deps) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	target, state, err := oidc.AuthorizeURL(r.Context(), d.DB, d.Settings, safeNext(r.URL.Query().Get("next")), nil)
	if err != nil {
		d.loginError(w, r, http.StatusServiceUnavailable, errKey(err))
		return
	}
	d.setOIDCState(w, state, oidcCookieAge)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// setOIDCState stores (age > 0) or clears (age < 0) the flow's state.
// Lax: the IdP's redirect back is a top-level GET, which Lax sends.
func (d Deps) setOIDCState(w http.ResponseWriter, state string, age int) {
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: state, Path: oidc.CallbackPath, MaxAge: age, HttpOnly: true,
		Secure: d.Settings.SecureCookies(), SameSite: http.SameSiteLaxMode})
}

func (d Deps) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	browser := oidc.Browser{}
	if c, err := r.Cookie(oidcCookie); err == nil {
		browser.State = c.Value
	}
	if ctx, err := d.Context(r); err == nil && ctx.Who != nil {
		browser.UserID = ctx.Who.UserID
	}
	d.setOIDCState(w, "", -1)

	result, err := oidc.Complete(r.Context(), d.DB, d.Settings, r.URL.Query(), browser)
	if err != nil {
		d.loginError(w, r, http.StatusUnauthorized, errKey(err))
		return
	}
	token, err := auth.OpenOIDCSession(d.DB, d.Settings, result.UserID, ClientIP(r), Agent(r), result.IDToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.setSession(w, token)
	next := result.Next
	if next == "" {
		next = startPath
	}
	http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
}

func (d Deps) handleOIDCLink(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	userID := ctx.Who.UserID
	target, state, err := oidc.AuthorizeURL(r.Context(), d.DB, d.Settings, profileHome, &userID)
	if err != nil {
		d.securityPage(w, ctx, http.StatusServiceUnavailable, map[string]any{"Error": errKey(err)})
		return
	}
	d.setOIDCState(w, state, oidcCookieAge)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// loginError shows the login page with a translated error.
func (d Deps) loginError(w http.ResponseWriter, r *http.Request, status int, key string) {
	ctx, _ := d.Context(r)
	_ = d.Page(w, ctx, "login", status, d.loginExtras(map[string]any{"Error": key}))
}
