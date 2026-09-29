package web

import (
	"net/http"
	"strconv"

	"andon/internal/enums"
	"andon/internal/services/accounts"
	"andon/internal/services/auth"
	"andon/internal/services/oidc"
	"andon/internal/services/passkeys"
)

// RegisterSecurityRoutes wires /me/security: password, TOTP, sessions, API tokens.
func (d Deps) RegisterSecurityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /me/security", d.authed(d.handleSecurityPage))
	mux.HandleFunc("POST /me/security/password", d.authed(d.handlePasswordChange))
	mux.HandleFunc("POST /me/security/totp/begin", d.authed(d.handleTOTPBeginForm))
	mux.HandleFunc("POST /me/security/totp/confirm", d.authed(d.handleTOTPConfirmForm))
	mux.HandleFunc("POST /me/security/totp/disable", d.authed(d.handleTOTPDisableForm))
	mux.HandleFunc("POST /me/security/sessions/{id}/end", d.authed(d.handleSessionEnd))
	mux.HandleFunc("POST /me/security/tokens", d.authed(d.handleTokenCreate))
	mux.HandleFunc("POST /me/security/tokens/{id}/revoke", d.authed(d.handleTokenRevoke))
}

func (d Deps) securityPage(w http.ResponseWriter, ctx Ctx, status int, extra map[string]any) {
	profile, err := accounts.GetProfile(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	sessions, err := auth.MySessions(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	tokens, err := auth.MyTokens(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	keys, err := passkeys.Mine(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	required, err := auth.TOTPRequired(d.DB, ctx.Who, ctx.Method)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{
		"Profile": profile, "Sessions": sessions, "Tokens": tokens, "OIDCLabel": oidc.Button(d.DB, d.Settings),
		"Passkeys": keys, "TOTPRequired": required,
	}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "security", status, values)
}

func (d Deps) handleSecurityPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.securityPage(w, ctx, http.StatusOK, nil)
}

func (d Deps) handlePasswordChange(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	var err error
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	err = accounts.ChangePassword(d.DB, ctx.Who, r.FormValue("current"), r.FormValue("new"), ClientIP(r))
	if err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

func (d Deps) handleTOTPBeginForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	secret, uri, err := auth.TOTPBegin(d.DB, ctx.Who)
	if err != nil {
		d.securityPage(w, ctx, http.StatusInternalServerError, map[string]any{"Error": errKey(err)})
		return
	}
	d.securityPage(w, ctx, http.StatusOK, map[string]any{"TOTPSecret": secret, "TOTPURI": uri})
}

func (d Deps) handleTOTPConfirmForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	codes, err := auth.TOTPConfirm(d.DB, ctx.Who, r.FormValue("code"), ClientIP(r))
	if err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.securityPage(w, ctx, http.StatusOK, map[string]any{"RecoveryCodes": codes})
}

func (d Deps) handleTOTPDisableForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := auth.TOTPDisable(d.DB, ctx.Who, r.FormValue("code"), ClientIP(r)); err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

func (d Deps) handleSessionEnd(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := auth.EndSession(d.DB, ctx.Who, id); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

func (d Deps) handleTokenCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	scope := enums.TokenScope(r.FormValue("scope"))
	if scope == "" {
		scope = enums.TokenRead
	}
	var days *int
	if raw := r.FormValue("days"); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			days = &n
		}
	}
	created, err := auth.CreateToken(d.DB, ctx.Who, r.FormValue("name"), scope, nil, days)
	if err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.securityPage(w, ctx, http.StatusOK, map[string]any{"NewToken": created.Secret})
}

func (d Deps) handleTokenRevoke(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := auth.RevokeToken(d.DB, ctx.Who, id); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}
