package sources

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

func dawarichAPI(sctx Ctx) (services.DawarichApi, error) {
	secret, err := needSecret(sctx)
	if err != nil {
		return services.DawarichApi{}, err
	}
	return services.DawarichApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}, nil
}

func dawarichVisit(raw any) DawarichVisit {
	m := asMap(raw)
	place := asMap(m["place"])
	area := asMap(m["area"])
	name := asStr(m["name"])
	if name == "" {
		name = asStr(place["name"])
	}
	if name == "" {
		name = asStr(area["name"])
	}
	areaID := asInt64(m["area_id"])
	if areaID == 0 {
		areaID = asInt64(area["id"])
	}
	v := DawarichVisit{
		ID: asInt64(m["id"]), Start: asStr(m["started_at"]), End: asStr(m["ended_at"]),
		Minutes: int(asFloat(m["duration"])), AreaID: areaID, Name: name,
	}
	if place["latitude"] != nil {
		lat := asFloat(place["latitude"])
		v.Lat = &lat
	}
	if place["longitude"] != nil {
		lon := asFloat(place["longitude"])
		v.Lon = &lon
	}
	return v
}

// DawarichData is the "dawarich.data" source: aggregates only — areas,
// visits, monthly distances, time of the last point.
var DawarichData = source{key: "dawarich.data", ttl: dataTTL, service: enums.ServiceDawarich, fetch: fetchDawarich}

func fetchDawarich(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDawarich(time.Now()), nil
	}
	api, err := dawarichAPI(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadDawarich(ctx, api, sctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadDawarich(ctx context.Context, api services.DawarichApi, sctx Ctx) (*DawarichDataset, error) {
	now := time.Now().UTC()
	// params.days widens the window (the tax year export asks for a year).
	days := max(visitDays, int(asFloat(sctx.Params["days"])))
	window := url.Values{
		"start_at": {now.AddDate(0, 0, -days).Format(time.RFC3339)},
		"end_at":   {now.Format(time.RFC3339)},
	}

	last, err := api.Get(ctx, "points", url.Values{"per_page": {"1"}, "order": {"desc"}})
	if err != nil {
		return nil, err
	}

	areasRaw, err := api.Get(ctx, "areas", nil)
	if err != nil {
		return nil, err
	}
	var areas []DawarichArea
	for _, a := range asList(areasRaw) {
		am := asMap(a)
		areas = append(areas, DawarichArea{
			ID: asInt64(am["id"]), Name: asStr(am["name"]), Lat: asFloat(am["latitude"]),
			Lon: asFloat(am["longitude"]), Radius: asFloat(am["radius"]),
		})
	}

	visitsRaw, err := api.Get(ctx, "visits", window)
	if err != nil {
		return nil, err
	}
	var visits []DawarichVisit
	for _, v := range asList(visitsRaw) {
		visits = append(visits, dawarichVisit(v))
	}

	statsRaw, err := api.Get(ctx, "stats", nil)
	if err != nil {
		return nil, err
	}

	data := &DawarichDataset{
		URL: sctx.URL, Areas: areas, Visits: visits, Stats: asMap(statsRaw), LastPoint: lastPoint(last),
		Places: loadDawarichPlaces(ctx, api),
	}
	loadTracks(ctx, api, tracksFrom(now, days), now, data)
	return data, nil
}

// tracksFrom is where the tracks start: the year so far, at least the
// visits' window (in January the last months still count).
func tracksFrom(now time.Time, days int) time.Time {
	year := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if back := now.AddDate(0, 0, -days); back.Before(year) {
		return back
	}
	return year
}

func lastPoint(points any) string {
	list := asList(points)
	if len(list) == 0 {
		return ""
	}
	first := asMap(list[0])
	if ts, ok := first["timestamp"].(float64); ok && ts != 0 {
		return time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
	}
	created := asStr(first["created_at"])
	return created
}

// RoutePoint is one tracked position.
type RoutePoint struct {
	Lat, Lon float64
	At       time.Time
}

// DawarichRoute is a day's track, oldest first.
type DawarichRoute struct{ Points []RoutePoint }

// A phone tracks every few seconds: a day is read in pages of routePage
// points (at most routePages) and thinned to routeMax for the map.
const (
	routePage  = 1000
	routePages = 20
	routeMax   = 2000
)

// DawarichRouteSource reads a day's points when the dialog opens:
// params from, to (RFC 3339).
var DawarichRouteSource = source{key: "dawarich.route", ttl: detailTTL, service: enums.ServiceDawarich, fetch: fetchDawarichRoute}

func fetchDawarichRoute(ctx context.Context, sctx Ctx) (any, error) {
	from, _ := time.Parse(time.RFC3339, asStr(sctx.Params["from"]))
	to, _ := time.Parse(time.RFC3339, asStr(sctx.Params["to"]))
	if isDemo(sctx) {
		return DemoDawarichRoute(from, to, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.DawarichApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	var all []any
	for page := 1; page <= routePages; page++ {
		raw, err := api.Get(ctx, "points", url.Values{"start_at": {from.Format(time.RFC3339)}, "end_at": {to.Format(time.RFC3339)},
			"per_page": {strconv.Itoa(routePage)}, "page": {strconv.Itoa(page)}, "order": {"asc"}})
		if err != nil {
			return nil, fetchError(err)
		}
		list := asList(raw)
		all = append(all, list...)
		if len(list) < routePage {
			break
		}
	}
	route := parseRoute(all)
	route.Points = thinRoute(route.Points, routeMax)
	return route, nil
}

// thinRoute keeps every nth point, first and last included, so at most
// limit remain: 5 points, limit 3 → 1st, 3rd, 5th.
func thinRoute(points []RoutePoint, limit int) []RoutePoint {
	if len(points) <= limit || limit < 2 {
		return points
	}
	step := float64(len(points)-1) / float64(limit-1)
	out := make([]RoutePoint, 0, limit)
	for i := range limit {
		out = append(out, points[int(float64(i)*step+0.5)])
	}
	return out
}

// parseRoute reads points ({latitude, longitude, timestamp}, numbers or
// strings), oldest first; points without a position are left out.
func parseRoute(raw any) *DawarichRoute {
	out := &DawarichRoute{}
	for _, p := range asList(raw) {
		m := asMap(p)
		pt := RoutePoint{Lat: asFloat(m["latitude"]), Lon: asFloat(m["longitude"]), At: time.Unix(asInt64(m["timestamp"]), 0).UTC()}
		if pt.Lat != 0 || pt.Lon != 0 {
			out.Points = append(out.Points, pt)
		}
	}
	sort.SliceStable(out.Points, func(i, j int) bool { return out.Points[i].At.Before(out.Points[j].At) })
	return out
}

// DawarichTest is the "dawarich.test" source: a lightweight connection check.
var DawarichTest = source{key: "dawarich.test", ttl: testTTL, service: enums.ServiceDawarich, fetch: fetchDawarichTest}

func fetchDawarichTest(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return map[string]any{"version": "demo"}, nil
	}
	api, err := dawarichAPI(sctx)
	if err != nil {
		return nil, err
	}
	v, err := api.Version(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return map[string]any{"version": v}, nil
}

func init() {
	Register(KimaiData)
	Register(DawarichRouteSource)
	Register(KimaiTest)
	Register(NinjaData)
	Register(NinjaTest)
	Register(SnipeData)
	Register(SnipeTest)
	Register(DawarichData)
	Register(DawarichTest)
}
