package web

import (
	"andon/internal/services/util"
	"net/http"
	"net/url"
	"strings"

	"andon/internal/enums"
	"andon/internal/services/connect"
	"andon/internal/services/connections"
)

// Sign in instead of a token: start, callback from the service, polling
// for link and code flows, and the OAuth client some services need.
//
//	form ─POST /connections/{id}/connect─► redirect to the service ─► callback ─► back
//	                                    └► wait page (link / code) ─poll─► back
const pollPath = "/connections/connect/poll"

// signIn is the sign-in column of a connection form.
type signIn struct {
	Method      connect.Method
	NeedsClient bool
	HasClient   bool
	Callback    string
}

func (d Deps) signInOf(conn connections.View) *signIn {
	method := connect.MethodOf(conn.Service, conn.URL, conn.Options)
	if method == connect.MethodNone {
		return nil
	}
	return &signIn{
		Method: method, NeedsClient: connect.NeedsClient(conn.Service),
		HasClient: connections.HasOAuthClient(d.DB, conn.ID),
		Callback:  strings.TrimRight(d.Settings.BaseURL, "/") + connect.CallbackPath,
	}
}

// canSignIn tells the new-connection form that signing in will be offered.
func canSignIn(service enums.ServiceType) bool {
	return connect.MethodOf(service, defaultURL(service), nil) != connect.MethodNone
}

// withQuery appends key=value to a local path, e.g. "/me/credentials?connected".
func withQuery(path, key, value string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	if value == "" {
		return path + sep + key
	}
	return path + sep + key + "=" + url.QueryEscape(value)
}

func (d Deps) handleConnectStart(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := recordPath(id, tabAccess)
	if b := r.FormValue("back"); b != "" {
		back = util.LocalPath(b)
	}

	step, err := connect.Start(r.Context(), d.DB, ctx.Who, d.Settings, id, back)
	switch {
	case err != nil:
		http.Redirect(w, r, withQuery(back, "error", errKey(err)), http.StatusSeeOther)
	case step.Redirect != "":
		http.Redirect(w, r, step.Redirect, http.StatusSeeOther)
	case step.Done:
		http.Redirect(w, r, withQuery(back, "connected", ""), http.StatusSeeOther)
	default:
		_ = d.Page(w, ctx, "connect_wait", http.StatusOK, map[string]any{"Step": step, "PollURL": pollPath + "?flow=" + url.QueryEscape(step.Flow), "Back": back})
	}
}

func (d Deps) handleConnectCallback(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	back, err := connect.Callback(r.Context(), d.DB, ctx.Who, d.Settings, r.URL.Query())
	if err != nil {
		http.Redirect(w, r, withQuery(back, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, withQuery(back, "connected", ""), http.StatusSeeOther)
}

// handleConnectPoll answers the wait page's htmx poll: nothing yet (204),
// or where to go now (HX-Redirect).
func (d Deps) handleConnectPoll(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	step, back, err := connect.Poll(r.Context(), d.DB, ctx.Who, r.URL.Query().Get("flow"))
	switch {
	case err != nil:
		w.Header().Set("HX-Redirect", withQuery(back, "error", errKey(err)))
	case step.Done:
		w.Header().Set("HX-Redirect", withQuery(back, "connected", ""))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleOAuthClient(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := recordPath(id, tabAccess)
	if err := connections.SetOAuthClient(d.DB, ctx.Who, id, strings.TrimSpace(r.FormValue("client_id")), strings.TrimSpace(r.FormValue("client_secret"))); err != nil {
		http.Redirect(w, r, withQuery(back, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
