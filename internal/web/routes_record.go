package web

import (
	"andon/internal/caps"
	"andon/internal/services/verbund"
	"net/http"
	"slices"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/homelable"
	"andon/internal/services/porting"
	"andon/internal/widgets"
)

// A connection's record: one page in the settings frame, its level's
// connections marked, with four tabs.
//
//	┌ Zeiterfassung · Kimai · kimai.lan        Instanz · Fest   ● 88 ms ┐
//	│ Überblick │ Zugang │ Einstellungen │ Verlauf                       │
//	│ what it is, who changes it, health, test, tiles to add             │
//	└────────────────────────────────────────────────────────────────────┘
//
// Every tab is rendered, the others hidden: a tab is a link (?tab=), and
// forms and messages land on the tab they belong to.

// Tabs of a connection's record.
const (
	tabOverview = "overview"
	tabAccess   = "access"
	tabSettings = "settings"
	tabHistory  = "history"
)

// recordDays is how far the history tab looks back.
const recordDays = 30

// navPath names the settings page a page counts as (render "at").
const navPath = "NavPath"

// recordPath is a record's tab, the overview without a tab.
func recordPath(id int64, tab string) string {
	path := "/connections/" + strconv.FormatInt(id, 10)
	if tab == "" || tab == tabOverview {
		return path
	}
	return path + "?tab=" + tab
}

// handleConnectionRecord shows a record on the tab the address names.
func (d Deps) handleConnectionRecord(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.recordPage(w, r, ctx, tabOverview, http.StatusOK, nil)
}

// recordPage renders the record of the path's connection on tab (or the
// one ?tab= names); extra adds or overrides values, e.g. a test result.
func (d Deps) recordPage(w http.ResponseWriter, r *http.Request, ctx Ctx, tab string, status int, extra map[string]any) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	conn, err := connections.Get(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	values, err := d.recordValues(r, ctx, conn)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	for k, v := range extra {
		values[k] = v
	}

	tabs := []string{tabOverview, tabAccess}
	if conn.Service == enums.ServiceDawarich {
		tabs = append(tabs, tabPlaces)
	}
	if conn.Service == enums.ServiceHomelable {
		tabs = append(tabs, tabSync)
	}
	if conn.Right >= enums.RightManage {
		tabs = append(tabs, tabSettings)
	}
	tabs = append(tabs, tabHistory)
	if asked := r.URL.Query().Get("tab"); asked != "" {
		tab = asked
	}
	if !slices.Contains(tabs, tab) {
		tab = tabOverview
	}
	values["Tabs"], values["Tab"] = tabs, tab
	_ = d.Page(w, ctx, "connection", status, values)
}

// recordValues is what a record shows besides its tabs.
func (d Deps) recordValues(r *http.Request, ctx Ctx, conn connections.View) (map[string]any, error) {
	q := r.URL.Query()
	values := map[string]any{
		"Conn": conn, "Services": serviceOptions, "IsNew": false,
		"OptionsYAML": porting.DumpMap(conn.Options), "Error": q.Get("error"), "Note": q.Get("note"),
		"SignIn": d.signInOf(conn), "CanManage": conn.Right >= enums.RightManage,
	}
	if ref, ok := ctx.Who.Spaces[conn.SpaceID]; ok {
		values["LevelName"] = ref.Name
		values[navPath] = spacePath(conn.SpaceID) + "/connections"
		// Only admins see the instance's page; others reach its templates
		// from their own.
		if mine := access.Personal(ctx.Who); ref.Kind == enums.SpaceInstance && !ctx.Who.IsAdmin() && mine != nil {
			values[navPath] = spacePath(mine.ID) + "/connections"
		}
	}

	if rows, err := connections.Capabilities(r.Context(), d.DB, ctx.Who, conn.ID); err == nil {
		values["Caps"] = rows
	}
	values["Traits"] = caps.TraitsOf(enums.ServiceType(conn.Service)).Keys()
	if groups, err := verbund.Of(d.DB, ctx.Who, conn.ID); err == nil {
		values["InVerbund"] = groups
	}

	history, err := connections.History(d.DB, ctx.Who, conn.ID, recordDays, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	values["History"], values["Days"], values["Reach"] = history, recordDays, 100-history.FailPct
	var cells []widgets.StripCell
	for _, day := range history.Days {
		state := widgets.CellState(widgets.ConnDayState{Day: day.Day, OK: day.OK, Fail: day.Fail})
		cells = append(cells, widgets.StripCell{State: state, Title: day.Day})
	}
	values["Cells"] = cells

	activations, err := connections.ActivationsOf(d.DB, ctx.Who, conn.ID)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, sp := range ctx.Who.Spaces {
		if sp.TeamID != nil {
			names[*sp.TeamID] = sp.Name
		}
	}
	values["Activations"], values["TeamNames"] = activations, names
	values["ActivationSignIn"] = map[int64]*signIn{conn.ID: d.signInOf(conn)}

	if conn.Service == enums.ServiceHomelable {
		values["Sync"], values["Synced"], _ = homelable.Last(d.DB, ctx.Who, conn.ID)
		values["SyncOpens"] = homelable.Opens(conn.URL)
	}
	// Only who may rotate the webhook sees its URL (it is the secret).
	if conn.Right >= enums.RightManage {
		values["HookURL"], _ = connections.HookURL(d.DB, ctx.Who, conn.ID, d.Settings.BaseURL)
	}
	// Just signed in, set up or given a new login: show right away whether
	// the service answers.
	if q.Has("connected") || q.Has("welcome") || q.Has(testedFlag) {
		if result, err := connections.Test(r.Context(), d.DB, ctx.Who, conn.ID); err == nil {
			d.addTest(values, ctx, conn, result)
		}
	}
	if q.Has("connected") {
		values["Connected"] = true
	}
	if q.Has("welcome") {
		values["Suggest"] = widgetsFor(conn.Service)
	}
	return values, nil
}

// testedFlag asks the record to run the connection test on load, e.g.
// after a new login was saved.
const testedFlag = "tested"

// addTest puts a test result into a record's values: the result, its
// explanation, and the tiles a green test offers.
func (d Deps) addTest(values map[string]any, ctx Ctx, conn connections.View, result connections.TestResult) {
	values["TestResult"], values["TestNote"] = result, noteOf(result, conn, ctx.Who)
	if result.Ok {
		values["Offer"] = d.offerFor(ctx, conn.ID)
	}
}

// handleConnectionSecret replaces a fixed connection's login (access tab).
func (d Deps) handleConnectionSecret(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	conn, err := connections.Get(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	secret, err := formSecret(r, conn.Service)
	if err == nil && secret != "" {
		tls := connections.TLSSkip
		if conn.VerifyTLS {
			tls = connections.TLSVerify
		}
		err = connections.Update(d.DB, ctx.Who, id, conn.Name, conn.URL, conn.Mode, &secret, tls, nil)
	}
	if err != nil {
		d.recordPage(w, r, ctx, tabAccess, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, withQuery(recordPath(id, tabAccess), testedFlag, ""), http.StatusSeeOther)
}
