package web

import (
	"andon/internal/services/connections"
	"net/http"
	"strconv"

	"andon/internal/enums"
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
}

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
	values := map[string]any{
		"Channels": chans, "Prefs": prefs, "Levels": severityLevels,
		"Weekdays": notify.Weekdays, "BaseURL": d.Settings.BaseURL, "SummaryAvailable": summary.Enabled(),
		"Services": d.usedServices(ctx),
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
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": err.Error()})
		return
	}
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
}

func (d Deps) handleNotifyChannelTest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := notify.TestChannel(r.Context(), d.DB, d.Settings, ctx.Who, id); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": err.Error()})
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
		Daily: r.FormValue("daily"), Weekly: r.FormValue("weekly"),
	}
	prefs.RepeatHours, _ = strconv.Atoi(r.FormValue("repeat_hours"))

	// The checkbox only exists while the instance has an API key.
	if summary.Enabled() {
		prefs.NoSummary = r.FormValue("summary") == ""
	}
	if err := notify.SavePrefs(d.DB, ctx.Who, prefs); err != nil {
		d.notifyPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": err.Error()})
		return
	}
	http.Redirect(w, r, "/me/notify", http.StatusSeeOther)
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
