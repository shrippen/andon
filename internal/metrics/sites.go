// Sites: the places rides start and end at, from three sources. Later
// ones refine earlier ones; a Kimai place without a Dawarich link adds a
// site of its own:
//
//	Dawarich areas, places ─► Kimai mileage places ─► Andon ("places" option)
//	     coordinates              kind, customer          kind, customer
package metrics

import (
	"fmt"
	"math"
	"sort"

	"andon/internal/sources"
)

// PlaceKind is what a site is to its owner. The zero value is a site
// nobody assigned yet.
type PlaceKind string

const (
	KindNone     PlaceKind = ""
	KindHome     PlaceKind = "home"
	KindWork     PlaceKind = "work"
	KindCustomer PlaceKind = "customer"
	KindPrivate  PlaceKind = "private"
	KindOther    PlaceKind = "other"
)

// PlaceKinds are the kinds a person picks from.
var PlaceKinds = []PlaceKind{KindHome, KindWork, KindCustomer, KindPrivate, KindOther}

// PlaceOrigin is who assigned a site's kind and customer.
type PlaceOrigin string

const (
	OriginNone   PlaceOrigin = ""
	OriginPlugin PlaceOrigin = "plugin"
	OriginAndon  PlaceOrigin = "andon"
)

const (
	// placeRadius is the radius of a Dawarich place (it has none) and of
	// a Kimai place without one, in metres.
	placeRadius = 100.0
	// siteSlack widens every radius: GPS ends a ride a little off.
	siteSlack = 50.0
)

// Site is one place a ride can start or end at. Key is "area:7",
// "place:11" or "kimai:5" (a Kimai place without Dawarich link).
type Site struct {
	Key        string
	Name       string
	Kind       PlaceKind
	CustomerID int64
	Lat, Lon   float64
	Radius     float64
	AreaID     int64
	PlaceID    int64
	PluginID   int64 // the Kimai mileage place, 0 if none
	Origin     PlaceOrigin
}

// Book is a Dawarich connection's sites.
type Book struct {
	Sites []*Site
	byKey map[string]*Site
}

// Assignment is what Andon stores for a site in the Dawarich
// connection's "places" option: {"area:7": {"kind": "customer",
// "customer_id": 12}}.
type Assignment struct {
	Kind       PlaceKind
	CustomerID int64
}

// AreaKey and PlaceKey are the keys of Dawarich areas and places.
func AreaKey(id int64) string  { return fmt.Sprintf("area:%d", id) }
func PlaceKey(id int64) string { return fmt.Sprintf("place:%d", id) }
func kimaiKey(id int64) string { return fmt.Sprintf("kimai:%d", id) }

// ParseAssignments decodes the "places" option of a Dawarich connection.
func ParseAssignments(options map[string]any) map[string]Assignment {
	raw, _ := options["places"].(map[string]any)
	out := map[string]Assignment{}
	for key, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		out[key] = Assignment{Kind: PlaceKind(kind), CustomerID: anyInt(m["customer_id"])}
	}
	return out
}

func anyInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// BookOf builds the sites of a Dawarich connection; geo and kimai may be
// nil.
func BookOf(geo *sources.DawarichDataset, kimai *sources.KimaiDataset, options map[string]any) *Book {
	b := &Book{byKey: map[string]*Site{}}
	if geo != nil {
		for _, a := range geo.Areas {
			b.add(&Site{Key: AreaKey(a.ID), Name: a.Name, Lat: a.Lat, Lon: a.Lon, Radius: a.Radius, AreaID: a.ID})
		}
		for _, p := range geo.Places {
			b.add(&Site{Key: PlaceKey(p.ID), Name: p.Name, Lat: p.Lat, Lon: p.Lon, Radius: placeRadius, PlaceID: p.ID})
		}
	}
	if kimai != nil {
		for _, p := range kimai.Places {
			b.plugin(p)
		}
	}

	// The old "areas" option names areas: {"Muster GmbH Büro": {"customer_id": 12}}.
	for name, m := range ParseAreaMapping(options) {
		for _, s := range b.Sites {
			if s.AreaID == 0 || s.Name != name {
				continue
			}
			s.Kind, s.CustomerID, s.Origin = KindCustomer, m.CustomerID, OriginAndon
			if m.Home {
				s.Kind, s.CustomerID = KindHome, 0
			}
		}
	}
	for key, a := range ParseAssignments(options) {
		s := b.byKey[key]
		if s == nil {
			continue
		}
		s.Kind, s.CustomerID, s.Origin = a.Kind, a.CustomerID, OriginAndon
	}
	return b
}

func (b *Book) add(s *Site) {
	if s.Radius <= 0 {
		s.Radius = placeRadius
	}
	b.Sites = append(b.Sites, s)
	b.byKey[s.Key] = s
}

// plugin merges a Kimai mileage place into the site it was imported
// from, or adds it as a site of its own.
func (b *Book) plugin(p sources.KimaiPlace) {
	var s *Site
	switch {
	case p.AreaID != 0:
		s = b.byKey[AreaKey(p.AreaID)]
	case p.PlaceID != 0:
		s = b.byKey[PlaceKey(p.PlaceID)]
	}
	if s == nil {
		if p.Lat == 0 && p.Lon == 0 {
			return
		}
		s = &Site{Key: kimaiKey(p.ID), Name: p.Name, Lat: p.Lat, Lon: p.Lon, Radius: p.Radius}
		b.add(s)
	}
	s.PluginID, s.Origin = p.ID, OriginPlugin
	s.Kind = PlaceKind(p.Type)
	if s.Kind == KindOther && p.CustomerID != 0 {
		s.Kind = KindCustomer
	}
	s.CustomerID = p.CustomerID
}

// Site is the site with this key, nil if none.
func (b *Book) Site(key string) *Site { return b.byKey[key] }

// At is the nearest site whose radius holds the point, nil if none.
func (b *Book) At(lat, lon float64) *Site {
	var best *Site
	bestM := math.Inf(1)
	for _, s := range b.Sites {
		m := DistanceKM(lat, lon, s.Lat, s.Lon) * metresPerKM
		if m <= s.Radius+siteSlack && m < bestM {
			best, bestM = s, m
		}
	}
	return best
}

// Home is the site assigned as home, nil if none.
func (b *Book) Home() *Site { return b.first(KindHome) }

// Work is the site assigned as the place of work, nil if none.
func (b *Book) Work() *Site { return b.first(KindWork) }

func (b *Book) first(kind PlaceKind) *Site {
	for _, s := range b.Sites {
		if s.Kind == kind {
			return s
		}
	}
	return nil
}

// visitSite is the site of a Dawarich visit: its area, else where it was.
func (b *Book) visitSite(v sources.DawarichVisit, areas []sources.DawarichArea) *Site {
	if a := areaOf(v, areas); a != nil {
		if s := b.byKey[AreaKey(a.ID)]; s != nil {
			return s
		}
	}
	if v.Lat == nil || v.Lon == nil {
		return nil
	}
	return b.At(*v.Lat, *v.Lon)
}

// Sorted is the sites by name.
func (b *Book) Sorted() []*Site {
	out := append([]*Site(nil), b.Sites...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
