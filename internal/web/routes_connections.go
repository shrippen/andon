package web

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"net/http"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/access"
	"andon/internal/services/connect"
	"andon/internal/services/connections"
	"andon/internal/services/places"
	"andon/internal/services/porting"
	"andon/internal/widgets"
)

// RegisterConnectionRoutes wires the connections list/create/edit/delete/test pages.
func (d Deps) RegisterConnectionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /connections", d.authed(d.handleConnectionsList))
	mux.HandleFunc("GET /connections/new", d.authed(d.handleConnectionNewForm))
	mux.HandleFunc("POST /connections", d.authed(d.handleConnectionCreate))
	mux.HandleFunc("GET /connections/{id}/edit", d.authed(d.handleConnectionEditForm))
	mux.HandleFunc("POST /connections/{id}/edit", d.authed(d.handleConnectionUpdate))
	mux.HandleFunc("POST /connections/{id}/delete", d.authed(d.handleConnectionDelete))
	mux.HandleFunc("POST /connections/{id}/test", d.authed(d.handleConnectionTest))
	mux.HandleFunc("POST /connections/{id}/check", d.authed(d.handleConnectionCheck))
	mux.HandleFunc("GET /places", d.authed(d.handlePlaces))
	mux.HandleFunc("POST /connections/{id}/hygiene", d.authed(d.handleConnectionHygiene))
	mux.HandleFunc("POST /connections/{id}/hook/rotate", d.authed(d.handleHookRotate))
	mux.HandleFunc("POST /connections/{id}/connect", d.authed(d.handleConnectStart))
	mux.HandleFunc("POST /connections/{id}/oauth-client", d.authed(d.handleOAuthClient))
	mux.HandleFunc("GET "+connect.CallbackPath, d.authed(d.handleConnectCallback))
	mux.HandleFunc("GET "+pollPath, d.authed(d.handleConnectPoll))
}

func (d Deps) handleConnectionsList(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := connections.Listing(d.DB, ctx.Who, enums.RightView)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	// Broken first, then shaky, not yet fetched, working; by name within.
	sort.SliceStable(list, func(i, j int) bool {
		return stateRank[list[i].Health.State()] < stateRank[list[j].Health.State()]
	})
	_ = d.Page(w, ctx, "connections", http.StatusOK, map[string]any{
		"Connections": list, "Services": serviceOptions, "Summary": healthSummary(list),
	})
}

// stateRank orders the connection list, what needs care first.
var stateRank = map[connections.HealthState]int{
	connections.HealthFailing: 0, connections.HealthShaky: 1, connections.HealthUnknown: 2, connections.HealthOK: 3,
}

// connSum counts connections by state for the line above the list.
type connSum struct{ Failing, Shaky, OK, Unknown int }

func healthSummary(list []connections.View) connSum {
	var sum connSum
	for _, c := range list {
		switch c.Health.State() {
		case connections.HealthFailing:
			sum.Failing++
		case connections.HealthShaky:
			sum.Shaky++
		case connections.HealthUnknown:
			sum.Unknown++
		default:
			sum.OK++
		}
	}
	return sum
}

var serviceOptions = enums.Services

func (d Deps) handleConnectionNewForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	// Step 1 of the assistant: pick the service; step 2: its form with
	// where to find the token; step 3 (edit page, ?welcome): test and
	// matching widgets.
	service := enums.ServiceType(r.URL.Query().Get("service"))
	if !service.Known() {
		picks, err := d.servicePicks(ctx)
		if err != nil {
			d.fail(w, err, http.StatusInternalServerError)
			return
		}
		_ = d.Page(w, ctx, "connection_pick", http.StatusOK, map[string]any{"Services": picks})
		return
	}
	spaces := access.EditableSpaces(ctx.Who)
	_ = d.Page(w, ctx, "connection_form", http.StatusOK, map[string]any{
		"Spaces": spaces, "Services": serviceOptions, "IsNew": true, "Service": service,
	})
}

// widgetsFor lists the widget types that show a service's data.
func widgetsFor(service enums.ServiceType) []widgets.WidgetType {
	var out []widgets.WidgetType
	for _, kind := range widgets.AllTypes() {
		if kind.Service == service {
			out = append(out, kind)
		}
	}
	return out
}

func (d Deps) handleConnectionCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	spaceID := formID(r, "space_id")
	tls := connections.TLSVerify
	if r.FormValue("tls") == "skip" {
		tls = connections.TLSSkip
	}
	mode := enums.CredentialMode(r.FormValue("mode"))
	if mode == "" {
		mode = enums.CredentialShared
	}
	service := enums.ServiceType(r.FormValue("service"))

	secret, err := formSecret(r, service)
	var id int64
	if err == nil {
		id, err = connections.Create(d.DB, ctx.Who, spaceID, service,
			r.FormValue("name"), r.FormValue("url"), mode, secret, tls, formOptions(r, service, nil))
	}
	if err != nil {
		_ = d.Page(w, ctx, "connection_form", http.StatusBadRequest, map[string]any{
			"Spaces": access.EditableSpaces(ctx.Who), "Services": serviceOptions, "IsNew": true, "Error": err.Error(),
			"Service": service,
		})
		return
	}
	http.Redirect(w, r, "/connections/"+strconv.FormatInt(id, 10)+"/edit?welcome", http.StatusSeeOther)
}

func (d Deps) handleConnectionEditForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
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
	values := map[string]any{
		"Conn": conn, "Services": serviceOptions, "IsNew": false,
		"OptionsYAML": porting.DumpMap(conn.Options), "Error": r.URL.Query().Get("error"),
		"SignIn": d.signInOf(conn),
	}
	// Only who may rotate the webhook sees its URL (it is the secret).
	if conn.Right >= enums.RightManage {
		values["HookURL"], _ = connections.HookURL(d.DB, ctx.Who, conn.ID, d.Settings.BaseURL)
	}
	// Just signed in: show right away whether the service answers.
	if r.URL.Query().Has("connected") {
		if result, err := connections.Test(r.Context(), d.DB, ctx.Who, id); err == nil {
			values["TestResult"] = result
		}
		values["Connected"] = true
	}
	if r.URL.Query().Has("welcome") {
		if result, err := connections.Test(r.Context(), d.DB, ctx.Who, id); err == nil {
			values["TestResult"] = result
		}
		values["Suggest"] = widgetsFor(conn.Service)
	}
	_ = d.Page(w, ctx, "connection_form", http.StatusOK, values)
}

func (d Deps) handleConnectionUpdate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	conn, err := connections.Get(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	tls := connections.TLSVerify
	if r.FormValue("tls") == "skip" {
		tls = connections.TLSSkip
	}
	mode := enums.CredentialMode(r.FormValue("mode"))
	if mode == "" {
		mode = enums.CredentialShared
	}
	// Switching shared ⇄ personal changes who can see what: explain first.
	if mode != conn.Mode && r.FormValue("mode_confirmed") == "" {
		entered, _ := formSecret(r, conn.Service)
		_ = d.Page(w, ctx, "connection_mode", http.StatusOK, map[string]any{
			"Conn": conn, "To": string(mode), "Name": r.FormValue("name"), "URL": r.FormValue("url"),
			"TLS": r.FormValue("tls"), "SecretDropped": entered != "",
		})
		return
	}

	var secret *string
	s, err := formSecret(r, conn.Service)
	if s != "" {
		secret = &s
	}
	if err == nil {
		err = connections.Update(d.DB, ctx.Who, id, r.FormValue("name"), r.FormValue("url"), mode, secret, tls,
			formOptions(r, conn.Service, conn.Options))
	}
	if err == nil && mode == enums.CredentialShared && r.FormValue("share_mine") != "" {
		err = connections.ShareMine(d.DB, ctx.Who, id)
	}
	if err == nil {
		err = d.saveAdvanced(r, ctx, conn)
	}
	if err != nil {
		_ = d.Page(w, ctx, "connection_form", http.StatusBadRequest, map[string]any{
			"Conn": conn, "Services": serviceOptions, "IsNew": false,
			"OptionsYAML": porting.DumpMap(conn.Options), "Error": errKey(err),
		})
		return
	}
	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

// saveAdvanced stores the form's "Erweitert" part: token expiry and
// daily budget when changed, the YAML options when edited (they win over
// the setup fields above).
func (d Deps) saveAdvanced(r *http.Request, ctx Ctx, conn connections.View) error {
	if _, sent := r.PostForm["budget"]; !sent {
		return nil
	}
	budget := formInt(r, "budget")
	if expires := r.FormValue("expires"); expires != conn.SecretExpires || budget != conn.DailyBudget {
		if err := connections.SetHygiene(d.DB, ctx.Who, conn.ID, expires, budget); err != nil {
			return err
		}
	}
	yaml := r.FormValue("options_yaml")
	if strings.TrimSpace(yaml) == strings.TrimSpace(r.FormValue("options_before")) {
		return nil
	}
	options, err := porting.Load(yaml)
	if err != nil {
		return err
	}
	return connections.SetOptions(d.DB, ctx.Who, conn.ID, options)
}

func (d Deps) handleConnectionDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := connections.Delete(d.DB, ctx.Who, id); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

func (d Deps) handleConnectionTest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	result, err := connections.Test(r.Context(), d.DB, ctx.Who, id)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	conn, _ := connections.Get(d.DB, ctx.Who, id)
	_ = d.Page(w, ctx, "connection_form", http.StatusOK, map[string]any{
		"Conn": conn, "Services": serviceOptions, "IsNew": false,
		"OptionsYAML": porting.DumpMap(conn.Options), "Error": r.URL.Query().Get("error"), "TestResult": result,
		"SignIn": d.signInOf(conn),
	})
}

// handleConnectionHygiene stores token expiry and daily fetch budget.
func (d Deps) handleConnectionHygiene(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	budget := formInt(r, "budget")
	target := "/connections/" + strconv.FormatInt(id, 10) + "/edit"
	err = connections.SetHygiene(d.DB, ctx.Who, id, r.FormValue("expires"), budget)
	if errors.Is(err, connections.ErrBadDate) {
		http.Redirect(w, r, target+"?error="+err.Error(), http.StatusSeeOther)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleHookRotate replaces a leaked webhook URL.
func (d Deps) handleHookRotate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := connections.RotateHook(d.DB, ctx.Who, id); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/connections/"+strconv.FormatInt(id, 10)+"/edit", http.StatusSeeOther)
}

// servicePick is one card of the service picker.
type servicePick struct {
	Service enums.ServiceType
	Name    string
	Count   int // connections of this service the caller can see
}

// servicePicks lists the services by shown name, e.g. "AdGuard Home"
// before "authentik", each with how many connections of it exist.
func (d Deps) servicePicks(ctx Ctx) ([]servicePick, error) {
	conns, err := connections.Listing(d.DB, ctx.Who, enums.RightView)
	if err != nil {
		return nil, err
	}
	count := map[enums.ServiceType]int{}
	for _, c := range conns {
		count[c.Service]++
	}

	picks := make([]servicePick, 0, len(serviceOptions))
	for _, s := range serviceOptions {
		picks = append(picks, servicePick{Service: s, Name: i18n.T("service."+string(s), ctx.Locale, nil), Count: count[s]})
	}
	sorter := collate.New(language.Make(string(ctx.Locale)), collate.IgnoreCase)
	sort.SliceStable(picks, func(a, b int) bool { return sorter.CompareString(picks[a].Name, picks[b].Name) < 0 })
	return picks, nil
}

// handleConnectionCheck runs the connection test from the overview and
// answers with the result only (htmx swaps it into the row).
func (d Deps) handleConnectionCheck(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := ""
	if conn, err := connections.Get(d.DB, ctx.Who, id); err == nil {
		name = conn.Name
	}
	result, err := connections.Test(r.Context(), d.DB, ctx.Who, id)
	if err != nil {
		result = connections.TestResult{Message: errKey(err)}
	}
	// The test counts as a fetch: its state replaces the list cell.
	values := map[string]any{"Result": result, "Name": name}
	if conn, err := connections.Get(d.DB, ctx.Who, id); err == nil {
		values["Conn"] = conn
	}
	_ = d.Page(w, ctx, "conn_check", http.StatusOK, values)
}

// handlePlaces answers the place search of a setup form with matching
// places to pick from.
func (d Deps) handlePlaces(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	query := r.URL.Query().Get("place_q")
	found, err := places.Search(r.Context(), query, ctx.Locale)
	if err != nil {
		d.fail(w, err, http.StatusBadGateway)
		return
	}
	_ = d.Page(w, ctx, "place_results", http.StatusOK, map[string]any{"Places": found, "Query": query})
}
