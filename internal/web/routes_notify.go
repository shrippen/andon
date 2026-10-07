package web

import (
	"andon/internal/services/connections"
	"net/http"
	"strconv"

	"andon/internal/enums"
	"andon/internal/services/mail"
	"andon/internal/services/notify"
	"andon/internal/services/summary"
)

// RegisterNotifyRoutes wires the "notifications" page under /me: channels
// (add/test/delete) and quiet-hours preferences.
func (d Deps) RegisterNotifyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /me/notify", d.authed(d.handleNotifyPage))
	mux.HandleFunc("POST /me/notify/channels", d.authed(d.handleNotifyChannelCreate))
	mux.HandleFunc("POST /me/notify/channels/{id}/test", d.authed(d.handleNotifyChannelTest))
	mux.HandleFunc("POST /me/notify/channels/{id}/delete", d.authed(d.handleNotifyChannelDelete))
	mux.HandleFunc("POST /me/notify/prefs", d.authed(d.handleNotifyPrefsSave))
	mux.HandleFunc("POST /me/notify/digest", d.authed(d.handleDigestSave))
	mux.HandleFunc("GET /me/notify/digest/preview", d.authed(d.handleDigestPreview))
	mux.HandleFunc("POST /me/notify/digest/test", d.authed(d.handleDigestTest))
}

// previewCSP lets the digest preview keep the mail's inline styles but
// run nothing.
const previewCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:"

var severityLevels = []enums.Severity{enums.SeverityInfo, enums.SeverityWarn, enums.SeverityCritical}

func (d Deps) notifyPage(w http.ResponseWriter, r *http.Request, ctx Ctx, status int, extra map[string]any) {
	chans, err := notify.Channels(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	prefs, err := notify.GetPrefs(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	digest, err := notify.GetDigest(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{
		"Channels": chans, "Prefs": prefs, "Levels": severityLevels,
		"Weekdays": notify.Weekdays, "BaseURL": d.Settings.BaseURL, "SummaryAvailable": summary.Enabled(),
		"Services": d.usedServices(ctx), "Ready": notify.Ready(d.live()),
		"Digest": digest, "MailReady": mail.Configured(),
	}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "notify", status, values)
}

func (d Deps) handleNotifyPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.notifyPage(w, r, ctx, http.StatusOK, nil)
}

func (d Deps) handleNotifyChannelCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	level := formInt(r, "level")
	sources := r.Form["sources"]
	if err := notify.AddChannel(d.DB, ctx.Who, r.FormValue("name"), r.FormValue("url"), enums.Severity(level), sources); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.flash(w, flashSaved)
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
}

func (d Deps) handleNotifyChannelTest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := notify.TestChannel(r.Context(), d.DB, d.live(), ctx.Who, id); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
}

func (d Deps) handleNotifyChannelDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := notify.DeleteChannel(d.DB, ctx.Who, id); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
}

func (d Deps) handleNotifyPrefsSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	prefs := notify.Prefs{
		QuietFrom: r.FormValue("quiet_from"), QuietTo: r.FormValue("quiet_to"), QuietMuted: r.FormValue("quiet_muted") != "",
	}
	prefs.RepeatHours, _ = strconv.Atoi(r.FormValue("repeat_hours"))
	if err := notify.SavePrefs(d.DB, ctx.Who, prefs); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.flash(w, flashSaved)
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
}

// digestAnchor returns to the mail card of the page.
const digestAnchor = "/me/notify#digest"

func (d Deps) handleDigestSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	old, err := notify.GetDigest(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	g := notify.Digest{
		Daily: r.FormValue("daily"), Weekly: r.FormValue("weekly"), MinLevel: enums.Severity(formInt(r, "level")),
		Deadlines: notify.DeadlinesOn, Empty: notify.EmptySend, NoSummary: old.NoSummary,
	}
	if r.FormValue("deadlines") == "" {
		g.Deadlines = notify.DeadlinesOff
	}
	if r.FormValue("empty") == "" {
		g.Empty = notify.EmptySkip
	}
	// The checkbox only exists while the instance has an API key; without
	// one the user's choice stays.
	if summary.Enabled() {
		g.NoSummary = r.FormValue("summary") == ""
	}
	if err := notify.SaveDigest(d.DB, ctx.Who, g); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.flash(w, flashSaved)
	http.Redirect(w, r, digestAnchor, http.StatusSeeOther)
}

// handleDigestPreview shows the digest mail as it would go out now.
func (d Deps) handleDigestPreview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	m, err := notify.DigestPreview(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Security-Policy", previewCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(m.HTML))
}

func (d Deps) handleDigestTest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := notify.DigestTest(d.DB, ctx.Who); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, digestAnchor, http.StatusSeeOther)
}

// usedServices are the services the caller has a connection to, in form
// order: the choices worth offering as notification sources.
func (d Deps) usedServices(ctx Ctx) []enums.ServiceType {
	conns, err := connections.Listing(d.DB, ctx.Who, enums.RightView)
	if err != nil || len(conns) == 0 {
		return enums.Services
	}
	used := map[enums.ServiceType]bool{}
	for _, c := range conns {
		used[c.Service] = true
	}
	var out []enums.ServiceType
	for _, s := range enums.Services {
		if used[s] {
			out = append(out, s)
		}
	}
	return out
}
