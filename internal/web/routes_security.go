package web

import (
	"bytes"
	"encoding/base64"
	"errors"
	"html/template"
	"image/png"
	"net/http"
	"strconv"

	"github.com/pquerna/otp"

	"andon/internal/enums"
	"andon/internal/services/accounts"
	"andon/internal/services/auth"
	"andon/internal/services/boards"
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
	// A form's answer page reloaded is a GET: back to the page of the form.
	for _, path := range []string{"/me/security/totp/begin", "/me/security/totp/confirm", "/me/security/totp/disable"} {
		mux.HandleFunc("GET "+path, formPage("/me/security"))
	}
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
	visible, err := boards.Visible(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{
		"Profile": profile, "Sessions": sessions, "Tokens": tokens, "OIDCLabel": oidc.Button(d.DB, d.live()),
		"Passkeys": keys, "TOTPRequired": required, "Boards": visible,
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
	err = accounts.ChangePassword(d.DB, ctx.Who, r.FormValue("current"), r.FormValue("new"), d.clientIP(r))
	if err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

func (d Deps) handleTOTPBeginForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	secret, uri, err := auth.TOTPBegin(d.DB, ctx.Who)
	if errors.Is(err, auth.ErrTOTPActive) {
		d.securityPage(w, ctx, http.StatusConflict, map[string]any{"Error": errKey(err)})
		return
	}
	if err != nil {
		d.securityPage(w, ctx, http.StatusInternalServerError, map[string]any{"Error": errKey(err)})
		return
	}
	d.securityPage(w, ctx, http.StatusOK, map[string]any{"TOTPSecret": secret, "TOTPURI": uri, "TOTPQR": qrImage(uri)})
}

// qrImage is the otpauth URI as a PNG data URL, for the app to scan.
func qrImage(uri string) template.URL {
	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		return ""
	}
	img, err := key.Image(qrSize, qrSize)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
}

// qrSize is the QR code's edge in pixels.
const qrSize = 200

func (d Deps) handleTOTPConfirmForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	codes, err := auth.TOTPConfirm(d.DB, ctx.Who, r.FormValue("code"), d.clientIP(r))
	if err != nil {
		values := map[string]any{"Error": errKey(err)}
		// The setup stays: same secret, scan once.
		if secret, uri, perr := auth.TOTPPending(d.DB, ctx.Who); perr == nil && secret != "" {
			values["TOTPSecret"], values["TOTPURI"], values["TOTPQR"] = secret, uri, qrImage(uri)
		}
		d.securityPage(w, ctx, http.StatusBadRequest, values)
		return
	}
	d.securityPage(w, ctx, http.StatusOK, map[string]any{"RecoveryCodes": codes})
}

func (d Deps) handleTOTPDisableForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := auth.TOTPDisable(d.DB, ctx.Who, r.FormValue("code"), d.clientIP(r)); err != nil {
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
	var boardIDs []int64
	for _, raw := range r.Form["board"] {
		if id, convErr := strconv.ParseInt(raw, 10, 64); convErr == nil {
			boardIDs = append(boardIDs, id)
		}
	}
	created, err := auth.CreateToken(d.DB, ctx.Who, r.FormValue("name"), scope, boardIDs, days)
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

// formPage answers a GET on a form's address (the reload of a page that
// answered the form) with the page of the form.
func formPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, http.StatusSeeOther)
	}
}
