package web

// The places tab of a Dawarich connection's record: which area or place
// is home, work, a client site or private, kept in step with the Kimai
// mileage plugin and Dawarich (services/sites). The tab loads its table
// when shown; forms post back and land on the tab.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/sites"
)

// tabPlaces is the places tab of a Dawarich record.
const tabPlaces = "places"

// RegisterSiteRoutes wires the places tab and its forms.
func (d Deps) RegisterSiteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /connections/{id}/places", d.authed(d.handleSites))
	mux.HandleFunc("POST /connections/{id}/places/assign", d.authed(d.handleSiteAssign))
	mux.HandleFunc("POST /connections/{id}/places/create", d.authed(d.handleSiteCreate))
	mux.HandleFunc("POST /connections/{id}/places/sync", d.authed(d.handleSiteSync))
	mux.HandleFunc("GET /connections/{id}/places/name", d.authed(d.handleSiteName))
	mux.HandleFunc("GET /travel/places", d.authed(d.handleTravelPlaces))
}

// handleSites renders the places table (loaded into the tab).
func (d Deps) handleSites(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view, err := sites.Overview(r.Context(), d.DB, ctx.Who, id)
	values := map[string]any{"Partial": true, "ConnID": id, "View": view}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "conn_places", http.StatusOK, values)
}

// placesBack returns to the tab, with an error or a note.
func placesBack(w http.ResponseWriter, r *http.Request, id int64, err error, note string) {
	q := url.Values{"tab": {tabPlaces}}
	if err != nil {
		q.Set("error", errKey(err))
	}
	if note != "" {
		q.Set("note", note)
	}
	http.Redirect(w, r, "/connections/"+strconv.FormatInt(id, 10)+"?"+q.Encode(), http.StatusSeeOther)
}

// assignment reads kind and customer of a form.
func assignment(r *http.Request) metrics.Assignment {
	kind := metrics.PlaceKind(r.FormValue("kind"))
	if !validKind(kind) {
		kind = metrics.KindNone
	}
	customer, _ := strconv.ParseInt(r.FormValue("customer"), 10, 64)
	return metrics.Assignment{Kind: kind, CustomerID: customer}
}

func validKind(kind metrics.PlaceKind) bool {
	for _, k := range metrics.PlaceKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (d Deps) handleSiteAssign(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = sites.Assign(r.Context(), d.DB, ctx.Who, id, r.FormValue("key"), assignment(r))
	placesBack(w, r, id, err, "")
}

func (d Deps) handleSiteCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := assignment(r)
	p := sites.NewPlace{Name: r.FormValue("name"), Lat: formFloat(r, "lat"), Lon: formFloat(r, "lon"), Radius: formFloat(r, "radius"),
		Kind: a.Kind, CustomerID: a.CustomerID}
	err = sites.Create(r.Context(), d.DB, ctx.Who, id, p)
	placesBack(w, r, id, err, "")
}

func (d Deps) handleSiteSync(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	res, err := sites.Sync(r.Context(), d.DB, ctx.Who, id)
	placesBack(w, r, id, err, strconv.Itoa(res.PluginPlaces)+"/"+strconv.Itoa(res.Areas))
}

// handleSiteName fills a new place's name field with what Dawarich calls
// the position; a name already typed stays.
func (d Deps) handleSiteName(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name, _ = sites.SuggestName(r.Context(), d.DB, ctx.Who, id, formFloat(r, "lat"), formFloat(r, "lon"))
	}
	_ = d.Page(w, ctx, "conn_place_name", http.StatusOK, map[string]any{"Partial": true, "Name": name})
}

func formFloat(r *http.Request, key string) float64 {
	n, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue(key)), ",", "."), 64)
	return n
}

// handleTravelPlaces opens the places tab of the caller's Dawarich
// connection: the personal space's first, else any.
func (d Deps) handleTravelPlaces(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	all, err := connections.Listing(d.DB, ctx.Who, enums.RightUse)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	var found int64
	mine := access.Personal(ctx.Who)
	for _, c := range all {
		if c.Service != enums.ServiceDawarich {
			continue
		}
		if found == 0 || (mine != nil && c.SpaceID == mine.ID) {
			found = c.ID
		}
	}
	if found == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, recordPath(found, tabPlaces), http.StatusSeeOther)
}
