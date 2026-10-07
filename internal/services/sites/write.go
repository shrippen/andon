package sites

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// NewPlace is a place to create where rides end: it becomes a Dawarich
// area and, with the plugin, a mileage place.
type NewPlace struct {
	Name       string
	Lat, Lon   float64
	Radius     float64
	Kind       metrics.PlaceKind
	CustomerID int64
}

// target is the write address of a connection with the caller's login.
func target(d *sql.DB, who *access.Principal, conn *model.Connection) (outbound.Target, error) {
	sctx, err := svcdata.SourceCtx(d, conn, model.UserHolder(who.UserID))
	if err != nil {
		return outbound.Target{}, err
	}
	return outbound.Target{URL: conn.URL, Token: sctx.Secret, VerifyTLS: conn.VerifyTLS}, nil
}

// forget drops both connections' cached data: the next read sees the
// change.
func (e env) forget() {
	svcdata.Forget(e.geoConn.ID)
	if e.kimaiConn != nil {
		svcdata.Forget(e.kimaiConn.ID)
	}
}

// kimaiTarget is the plugin's write address; EDIT on Kimai is needed.
func (e env) kimaiTarget(d *sql.DB, who *access.Principal) (outbound.Target, error) {
	if err := connections.Writable(d, who, e.kimaiConn.ID); err != nil {
		return outbound.Target{}, err
	}
	return target(d, who, e.kimaiConn)
}

// mileagePlace is what the plugin stores for a site of this kind.
func mileagePlace(s *metrics.Site, a metrics.Assignment) outbound.MileagePlace {
	p := outbound.MileagePlace{Name: s.Name, Type: pluginTypes[a.Kind], CustomerID: a.CustomerID,
		Lat: s.Lat, Lon: s.Lon, Radius: s.Radius, AreaID: s.AreaID, PlaceID: s.PlaceID}
	if a.Kind != metrics.KindCustomer || a.CustomerID == 0 {
		p = p.ClearCustomer()
	}
	return p
}

// Assign sets what a site is. Requires MANAGE on Dawarich (and EDIT on
// Kimai when the plugin stores it).
func Assign(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, key string, a metrics.Assignment) error {
	e, err := open(ctx, d, who, connID, enums.RightManage)
	if err != nil {
		return err
	}
	site := e.book.Site(key)
	if site == nil {
		return ErrUnknownPlace
	}
	if a.Kind != metrics.KindCustomer {
		a.CustomerID = 0
	}
	defer e.forget()

	stored := false
	if e.pluginWrites() {
		to, err := e.kimaiTarget(d, who)
		if err != nil {
			return err
		}
		if site.PluginID != 0 {
			err = outbound.KimaiUpdatePlace(ctx, to, site.PluginID, mileagePlace(site, a))
		} else {
			_, err = outbound.KimaiCreatePlace(ctx, to, mileagePlace(site, a))
		}
		if err != nil {
			return err
		}
		stored = e.kindHolder(a.Kind) == kimaiHolder
	}
	return e.keep(d, who, key, a, stored)
}

// keep writes the assignment to the connection's option, or removes it
// there when the plugin holds it.
func (e env) keep(d *sql.DB, who *access.Principal, key string, a metrics.Assignment, inPlugin bool) error {
	options := map[string]any{}
	for k, v := range e.geoConn.Options {
		options[k] = v
	}
	all := map[string]any{}
	if old, ok := options[optionPlaces].(map[string]any); ok {
		for k, v := range old {
			all[k] = v
		}
	}
	switch {
	case inPlugin || a.Kind == metrics.KindNone:
		delete(all, key)
	default:
		entry := map[string]any{"kind": string(a.Kind)}
		if a.CustomerID != 0 {
			entry["customer_id"] = float64(a.CustomerID)
		}
		all[key] = entry
	}
	options[optionPlaces] = all
	if err := connections.SetOptions(d, who, e.geoConn.ID, options); err != nil {
		return err
	}
	e.geoConn.Options = options // the next keep of a sync builds on it
	return nil
}

// Create adds a place where rides end: a Dawarich area, then its
// assignment (in the plugin or the option). Requires MANAGE.
func Create(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, p NewPlace) error {
	e, err := open(ctx, d, who, connID, enums.RightManage)
	if err != nil {
		return err
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || (p.Lat == 0 && p.Lon == 0) {
		return ErrBadPlace
	}
	if p.Radius <= 0 {
		p.Radius = defaultRadius
	}
	if p.Kind != metrics.KindCustomer {
		p.CustomerID = 0
	}
	if sources.IsDemo(e.geoConn.URL) {
		return ErrDemo
	}
	defer e.forget()

	to, err := target(d, who, e.geoConn)
	if err != nil {
		return err
	}
	areaID, err := outbound.DawarichCreateArea(ctx, to, outbound.Area{Name: p.Name, Lat: p.Lat, Lon: p.Lon, Radius: p.Radius})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDawarich, err)
	}
	site := &metrics.Site{Key: metrics.AreaKey(areaID), Name: p.Name, Lat: p.Lat, Lon: p.Lon, Radius: p.Radius, AreaID: areaID}
	a := metrics.Assignment{Kind: p.Kind, CustomerID: p.CustomerID}

	stored := false
	if e.pluginWrites() {
		kto, err := e.kimaiTarget(d, who)
		if err != nil {
			return err
		}
		if _, err := outbound.KimaiCreatePlace(ctx, kto, mileagePlace(site, a)); err != nil {
			return fmt.Errorf("%w: %v", ErrKimai, err)
		}
		stored = e.kindHolder(a.Kind) == kimaiHolder
	}
	return e.keep(d, who, site.Key, a, stored)
}

// SyncResult counts what a sync created.
type SyncResult struct {
	PluginPlaces, Areas int
}

// Sync brings Dawarich and the plugin in step: every Dawarich area and
// place gets a plugin place (with Andon's assignment, which then leaves
// the option), every plugin place without Dawarich link an area.
// Requires MANAGE on Dawarich and EDIT on Kimai.
func Sync(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) (SyncResult, error) {
	var out SyncResult
	e, err := open(ctx, d, who, connID, enums.RightManage)
	if err != nil || !e.pluginWrites() {
		return out, err
	}
	if sources.IsDemo(e.geoConn.URL) {
		return out, ErrDemo
	}
	kto, err := e.kimaiTarget(d, who)
	if err != nil {
		return out, err
	}
	gto, err := target(d, who, e.geoConn)
	if err != nil {
		return out, err
	}
	defer e.forget()

	assigned := metrics.ParseAssignments(e.geoConn.Options)
	for _, s := range e.book.Sites {
		switch {
		case s.PluginID == 0:
			a := assigned[s.Key]
			if _, err := outbound.KimaiCreatePlace(ctx, kto, mileagePlace(s, a)); err != nil {
				return out, fmt.Errorf("%w: %v", ErrKimai, err)
			}
			out.PluginPlaces++
			if a.Kind != metrics.KindNone && e.kindHolder(a.Kind) == kimaiHolder {
				if err := e.keep(d, who, s.Key, a, true); err != nil {
					return out, err
				}
			}
		case s.AreaID == 0 && s.PlaceID == 0:
			id, err := outbound.DawarichCreateArea(ctx, gto, outbound.Area{Name: s.Name, Lat: s.Lat, Lon: s.Lon, Radius: s.Radius})
			if err != nil {
				return out, fmt.Errorf("%w: %v", ErrDawarich, err)
			}
			if err := outbound.KimaiUpdatePlace(ctx, kto, s.PluginID, outbound.MileagePlace{AreaID: id}); err != nil {
				return out, fmt.Errorf("%w: %v", ErrKimai, err)
			}
			out.Areas++
		}
	}
	return out, nil
}
