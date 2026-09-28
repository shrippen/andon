package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"andon/internal/services/auth"
	"andon/internal/services/passkeys"
)

// Request bodies (attestation, assertion) stay far below this.
const passkeyBodyMax = 64 << 10

const jsonType = "application/json"

// RegisterPasskeyRoutes wires passkey registration (/me/passkeys) and
// login (/login/passkey). Both ceremonies are JSON, driven by passkeys.js.
func (d Deps) RegisterPasskeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /me/passkeys/begin", d.authed(d.handlePasskeyBegin))
	mux.HandleFunc("POST /me/passkeys/finish", d.authed(d.handlePasskeyFinish))
	mux.HandleFunc("POST /me/passkeys/{id}/delete", d.authed(d.handlePasskeyDelete))
	mux.HandleFunc("POST /login/passkey/begin", d.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /login/passkey/finish", d.handlePasskeyLoginFinish)
}

func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", jsonType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// jsonError answers with {"error": "<catalog key>"}.
func jsonError(w http.ResponseWriter, status int, key string) {
	body, _ := json.Marshal(map[string]string{"error": key})
	writeRawJSON(w, status, body)
}

func (d Deps) handlePasskeyBegin(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	options, err := passkeys.Begin(d.DB, d.Settings, ctx.Who)
	if err != nil {
		jsonError(w, http.StatusBadRequest, errKey(err))
		return
	}
	writeRawJSON(w, http.StatusOK, options)
}

func (d Deps) handlePasskeyFinish(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	var err error
	body := http.MaxBytesReader(w, r.Body, passkeyBodyMax)
	err = passkeys.Finish(d.DB, d.Settings, ctx.Who, r.URL.Query().Get("name"), body, ClientIP(r))
	if err != nil {
		jsonError(w, http.StatusBadRequest, errKey(err))
		return
	}
	writeRawJSON(w, http.StatusOK, []byte(`{"redirect":"/me/security"}`))
}

func (d Deps) handlePasskeyDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := passkeys.Remove(d.DB, ctx.Who, id, ClientIP(r)); err != nil {
		d.securityPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

// loginCeremony carries the login ceremony id between begin and finish.
type loginCeremony struct {
	Ceremony string          `json:"ceremony"`
	Options  json.RawMessage `json:"options"`
}

func (d Deps) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	id, options, err := passkeys.BeginLogin(d.Settings)
	if err != nil {
		jsonError(w, http.StatusTooManyRequests, errKey(err))
		return
	}
	body, _ := json.Marshal(loginCeremony{Ceremony: id, Options: options})
	writeRawJSON(w, http.StatusOK, body)
}

func (d Deps) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	// A JSON content type needs a CORS preflight, so a foreign page cannot
	// post an assertion here to log the visitor into another account.
	if r.Header.Get("Content-Type") != jsonType {
		jsonError(w, http.StatusUnsupportedMediaType, "login.failed")
		return
	}
	body := http.MaxBytesReader(w, r.Body, passkeyBodyMax)
	token, err := passkeys.FinishLogin(d.DB, d.Settings, r.URL.Query().Get("ceremony"), body, ClientIP(r), Agent(r))
	if err != nil {
		key := "login.failed"
		if errors.Is(err, auth.ErrOIDCOnly) {
			key = "login.oidc_only"
		}
		jsonError(w, http.StatusUnauthorized, key)
		return
	}
	d.setSession(w, token)
	writeRawJSON(w, http.StatusOK, []byte(`{"redirect":"`+startPath+`"}`))
}
