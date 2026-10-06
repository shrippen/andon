// Package sites keeps the places of a Dawarich connection: which area or
// place is home, work, a client site or private, and in step with the
// Kimai mileage plugin and Dawarich.
//
//	         assign / create / sync
//	web ──────────────► sites ──► outbound: Dawarich area, Kimai place
//	                        │
//	                        └────► connection option "places" (without plugin)
//
// With the plugin (feature placesWrite) the plugin stores the mapping and
// Andon keeps no copy; only "private", which the plugin lacks, stays in
// the option. Nothing is ever deleted across the services.
package sites

import (
	"andon/internal/caps"
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

var (
	// ErrNotDawarich: the connection is no Dawarich connection.
	ErrNotDawarich = errors.New("sites.not_dawarich")
	// ErrNoData: Dawarich did not answer yet.
	ErrNoData = errors.New("sites.no_data")
	// ErrUnknownPlace: the site is not (or no longer) there.
	ErrUnknownPlace = errors.New("sites.unknown")
	// ErrBadPlace: a new place lacks a name or a position.
	ErrBadPlace = errors.New("sites.bad")
)

const (
	optionPlaces = "places"
	// lookback is how far the overview counts rides.
	lookback = 90
	// unplacedMin is how often an unknown destination must recur.
	unplacedMin = 2
	// defaultRadius of a new place, metres.
	defaultRadius = 150.0
)

// pluginTypes are the mileage plugin's place types per kind; "private"
// has none there.
var pluginTypes = map[metrics.PlaceKind]string{
	metrics.KindHome: "home", metrics.KindWork: "work", metrics.KindCustomer: "customer",
	metrics.KindOther: "other", metrics.KindNone: "other", metrics.KindPrivate: "other",
}

// Row is one site with how often rides start or end there. Suggest is
// a Kimai customer whose name matches an unassigned site's, 0 if none.
type Row struct {
	*metrics.Site
	Rides   int
	KM      float64
	Last    time.Time
	Suggest int64
}

// suggestMin is the shortest name that is matched to a customer.
const suggestMin = 3

// suggest finds the customer whose name holds the site's, or the other
// way round ("Acme" ↔ "Acme GmbH").
func suggest(name string, customers []sources.KimaiCustomer) int64 {
	site := strings.ToLower(strings.TrimSpace(name))
	if len([]rune(site)) < suggestMin {
		return 0
	}
	for _, c := range customers {
		cust := strings.ToLower(strings.TrimSpace(c.Name))
		if len([]rune(cust)) >= suggestMin && (strings.Contains(cust, site) || strings.Contains(site, cust)) {
			return c.ID
		}
	}
	return 0
}

// View is a Dawarich connection's places.
type View struct {
	Rows      []Row
	Unplaced  []metrics.Destination
	Customers []sources.KimaiCustomer
	Kinds     []metrics.PlaceKind
	Plugin    bool // the Kimai mileage plugin answers
	Writes    bool // it creates and changes places
	CanManage bool
	Estimated bool // no tracks: rides from visits
}

// env is what every call reads: both connections and their data.
type env struct {
	geoConn, kimaiConn *model.Connection
	geo                *sources.DawarichDataset
	kimai              *sources.KimaiDataset
	book               *metrics.Book
	canManage          bool
}

func open(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, need enums.Right) (env, error) {
	view, err := connections.Get(d, who, connID)
	if err != nil {
		return env{}, err
	}
	if view.Service != enums.ServiceDawarich {
		return env{}, ErrNotDawarich
	}
	if err := access.Need(view.Right, need); err != nil {
		return env{}, err
	}
	e := env{canManage: view.Right >= enums.RightManage}
	if e.geoConn, err = connections.ByID(d, connID); err != nil || e.geoConn == nil {
		return env{}, ErrNotDawarich
	}
	res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceDawarich), nil, e.geoConn, model.UserHolder(who.UserID), svcdata.Cached)
	if err != nil {
		return env{}, err
	}
	geo, ok := res.Data.(*sources.DawarichDataset)
	if !ok {
		return env{}, ErrNoData
	}
	e.geo = geo

	// The Kimai connection of the same space, if any.
	all, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return env{}, err
	}
	for _, c := range all {
		if c.SpaceID != view.SpaceID || c.Service != enums.ServiceKimai {
			continue
		}
		if e.kimaiConn, err = connections.ByID(d, c.ID); err != nil {
			return env{}, err
		}
		if res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKimai), nil, e.kimaiConn, model.UserHolder(who.UserID), svcdata.Cached); err == nil {
			e.kimai, _ = res.Data.(*sources.KimaiDataset)
		}
		break
	}
	e.book = metrics.BookOf(e.geo, e.kimai, e.geoConn.Options)
	return e, nil
}

// pluginLive says whether the Kimai connection takes writes at all.
func (e env) pluginLive() bool {
	return e.kimai != nil && e.kimaiConn != nil && !sources.IsDemo(e.kimaiConn.URL)
}

// kimaiHolder is the Kimai connection as a caps holder.
var kimaiHolder = caps.HolderOf(enums.ServiceKimai)

// pluginWrites says whether places go to the mileage plugin, whatever
// their kind: a private place goes as "other".
func (e env) pluginWrites() bool {
	return e.pluginLive() && e.kimai.Caps.Can(caps.Places, caps.Update, "")
}

// kindHolder is who keeps what a place is: the plugin for its kinds,
// Andon for "private" and without the plugin.
func (e env) kindHolder(kind metrics.PlaceKind) caps.Holder {
	if !e.pluginLive() {
		return caps.Andon
	}
	return caps.Store(caps.Places, caps.Update, string(kind), e.kimai.Caps)
}

// Overview lists the sites, busiest first, and frequent destinations
// without one. Requires USE.
func Overview(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) (View, error) {
	e, err := open(ctx, d, who, connID, enums.RightUse)
	if err != nil {
		return View{}, err
	}
	// Where rides end does not depend on the space's travel settings.
	set := metrics.TravelSettingsOf(nil)
	travel := metrics.TravelOf(e.geo, e.kimai, e.geoConn.Options, set, time.Now())
	now := time.Now().UTC()
	rides := travel.Between(now.AddDate(0, 0, -lookback), now)

	used := map[string]*Row{}
	for _, dest := range metrics.Destinations(rides) {
		if dest.Site == nil {
			continue
		}
		used[dest.Site.Key] = &Row{Site: dest.Site, Rides: dest.Rides, KM: dest.KM, Last: dest.Last}
	}
	v := View{Kinds: metrics.PlaceKinds, CanManage: e.canManage, Estimated: travel.Estimated, Unplaced: metrics.Unplaced(rides, unplacedMin)}
	for _, s := range e.book.Sites {
		if r, ok := used[s.Key]; ok {
			v.Rows = append(v.Rows, *r)
			continue
		}
		v.Rows = append(v.Rows, Row{Site: s})
	}
	sort.SliceStable(v.Rows, func(i, j int) bool {
		if v.Rows[i].Rides != v.Rows[j].Rides {
			return v.Rows[i].Rides > v.Rows[j].Rides
		}
		return v.Rows[i].Name < v.Rows[j].Name
	})
	if e.kimai != nil {
		v.Customers, v.Plugin = e.kimai.Customers, e.kimai.Caps.Can(caps.Places, caps.Read, "")
		for i := range v.Rows {
			if v.Rows[i].Kind == metrics.KindNone {
				v.Rows[i].Suggest = suggest(v.Rows[i].Name, v.Customers)
			}
		}
	}
	v.Writes = e.pluginWrites()
	return v, nil
}
