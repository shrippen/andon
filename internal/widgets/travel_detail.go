package widgets

// The travel dialog: this month's rides (list and detail, with the ride
// on a map), business against private, the year by month and class, when
// rides happen, per customer, destinations, modes, allowances and what
// the data lacks.

import (
	"fmt"
	"math"
	"sort"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	// travelTop caps destination and customer tables.
	travelTop = 8
	// rideTime is a ride's clock time in lists.
	rideTime = "15:04"
	// unplacedMin is how often a destination must recur to be named.
	unplacedMin = 2
)

// classState colours a ride by class in lists.
var classState = map[metrics.RideClass]string{metrics.ClassBusiness: "ok", metrics.ClassCommute: "info", metrics.ClassPrivate: "off"}

// classSeries is the data colour of a class in graphs.
var classSeries = map[metrics.RideClass]string{metrics.ClassBusiness: "s1", metrics.ClassCommute: "s2", metrics.ClassPrivate: "s3"}

func siteLabel(s *metrics.Site) string {
	if s == nil {
		return "?"
	}
	return s.Name
}

// rideState is a ride's light: unconfirmed business rides are yellow.
func rideState(r metrics.ClassedRide) string {
	if r.Unconfirmed {
		return "warn"
	}
	return classState[r.Class]
}

// rideKey identifies a ride in the list.
func rideKey(r metrics.ClassedRide) string { return fmt.Sprint(r.Start.Unix()) }

func travelDetail(cfg TravelConfig, data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) DetailView {
	today := todayOf(ctx)
	travel := travelOf(data, ctx, results)
	set := metrics.TravelSettingsOf(ctx.Settings)
	rate := travelRate(cfg, ctx)
	kimai, _ := results[peerKimai].(*sources.KimaiDataset)
	names := map[int64]string{}
	if kimai != nil {
		names = metrics.KimaiCustomerNames(kimai)
	}

	start, _, _ := travelPeriod(cfg, today)
	period := travel.Between(start, today)
	sums := metrics.ByClass(period)
	allowance := 0.0
	for _, d := range metrics.Allowances(travel.Rides, set.Base) {
		if !d.Day.Before(start) {
			allowance += d.Amount()
		}
	}
	body := &DetailBody{Facts: []Kpi{
		{Value: NumU(sums[metrics.ClassBusiness].KM, 0, "km"), Label: T("detail.travel.business")},
		{Value: Money(sums[metrics.ClassBusiness].PayKM*rate, ""), Label: T("detail.travel.money"), Tier: "cyan"},
		{Value: NumU(sums[metrics.ClassPrivate].KM, 0, "km"), Label: T("detail.travel.private")},
		{Value: Money(allowance, ""), Label: T("detail.travel.allowance")},
	}}
	if travel.Estimated {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.travel.estimated")})
	}
	if travel.Partial {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.travel.partial")})
	}

	body.List, body.Blocks = rideList(period, names, results, body.Blocks)
	body.Blocks = append(body.Blocks, classBars(sums))
	body.Blocks = append(body.Blocks, travelYear(travel, data, today)...)
	booked := map[int64]float64{}
	if kimai != nil {
		booked = metrics.BookedMinutes(kimai, start, today)
	}
	body.Blocks = append(body.Blocks, travelTables(period, names, booked, rate)...)
	body.Blocks = append(body.Blocks, travelFacts(period, kimai, set, results, start)...)
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// rideList is the period's rides, newest first, and the picked one in
// detail with a map.
func rideList(rides []metrics.ClassedRide, names map[int64]string, results map[string]any, blocks []Block) (*ObjList, []Block) {
	if len(rides) == 0 {
		return nil, append(blocks, Block{Kind: BlockText, Data: Txt("detail.travel.none")})
	}
	list := &ObjList{Label: T("detail.travel.trips")}
	keys := make([]string, 0, len(rides))
	for i := len(rides) - 1; i >= 0; i-- {
		r := rides[i]
		keys = append(keys, rideKey(r))
		list.Items = append(list.Items, LitRow{
			Name: TxtA("detail.travel.ride", "time", r.Start.In(time.Local).Format(rideTime), "from", siteLabel(r.From), "to", siteLabel(r.To)),
			Meta: NumU(r.KM, 1, "km"), State: rideState(r), Item: keys[len(keys)-1],
		})
	}
	list.Sel = pickIndex(results, keys)
	r := pickedRide(rides, results)
	list.Title, list.Sub = Day(r.Day()), TxtA("ride.reason."+string(r.Reason))
	list.State, list.StateText = rideState(r), T("ride.class."+string(r.Class))

	customer := "–"
	if r.CustomerID != 0 {
		customer = names[r.CustomerID]
	}
	rows := [][]Cell{
		{{Value: Txt("detail.travel.from")}, {Value: siteLabel(r.From)}},
		{{Value: Txt("detail.travel.to")}, {Value: siteLabel(r.To)}},
		{{Value: Txt("detail.travel.time")}, {Value: r.Start.In(time.Local).Format(rideTime) + "–" + r.End.In(time.Local).Format(rideTime)}},
		{{Value: Txt("detail.travel.km")}, {Value: NumU(r.KM, 1, "km")}},
		{{Value: Txt("detail.travel.mode")}, {Value: Txt("ride.mode." + r.Mode)}},
		{{Value: Txt("detail.travel.customer")}, {Value: customer}},
	}
	if r.Unconfirmed {
		rows = append(rows, []Cell{{Value: Txt("detail.travel.unconfirmed")}, {Value: Txt("detail.travel.unconfirmed_why"), State: "warn"}})
	}
	blocks = append(blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: rows}})

	from, to := GeoPoint{Lat: r.FromLat, Lon: r.FromLon}, GeoPoint{Lat: r.ToLat, Lon: r.ToLon}
	if m, ok := NewMap(ridePath(r, results), []MapMark{Pin(from, siteLabel(r.From), "ok"), Pin(to, siteLabel(r.To), rideState(r))}); ok {
		blocks = append(blocks, Block{Kind: BlockMap, Label: T("detail.travel.map"), Data: m})
	}
	return list, blocks
}

// rideRouteName is the result name of the picked ride's points.
const rideRouteName = "route"

// pickedRide is the ride the viewer picked, else the newest.
func pickedRide(rides []metrics.ClassedRide, results map[string]any) metrics.ClassedRide {
	keys := make([]string, len(rides))
	for i := range rides {
		keys[i] = rideKey(rides[len(rides)-1-i])
	}
	return rides[len(rides)-1-pickIndex(results, keys)]
}

// rideRoute reads the points of the ride the dialog shows, so its map
// draws the road taken instead of a straight line.
func rideRoute(cfg TravelConfig, results map[string]any, ctx ViewCtx) []Query {
	data, ok := results[dataName].(*sources.DawarichDataset)
	if !ok {
		return nil
	}
	start, _, _ := travelPeriod(cfg, todayOf(ctx))
	rides := travelOf(data, ctx, results).Between(start, todayOf(ctx))
	if len(rides) == 0 {
		return nil
	}
	r := pickedRide(rides, results)
	return []Query{{Name: rideRouteName, Source: "dawarich.route", Conn: ConnWidget,
		Params: map[string]any{"from": r.Start.UTC().Format(time.RFC3339), "to": r.End.UTC().Format(time.RFC3339)}}}
}

// ridePath is the ride's tracked points, start and end alone when none
// were read.
func ridePath(r metrics.ClassedRide, results map[string]any) []GeoPoint {
	path := []GeoPoint{{Lat: r.FromLat, Lon: r.FromLon}}
	if route, ok := results[rideRouteName].(*sources.DawarichRoute); ok {
		for _, p := range route.Points {
			if p.At.Before(r.Start) || p.At.After(r.End) {
				continue
			}
			path = append(path, GeoPoint{Lat: p.Lat, Lon: p.Lon})
		}
	}
	return append(path, GeoPoint{Lat: r.ToLat, Lon: r.ToLon})
}

// classBars is km per class as share bars.
func classBars(sums map[metrics.RideClass]metrics.RideSum) Block {
	top := 0.0
	for _, s := range sums {
		top = max(top, s.KM)
	}
	var bars []ShareBar
	for _, c := range metrics.RideClasses {
		s := sums[c]
		if s.Rides == 0 {
			continue
		}
		bars = append(bars, ShareBar{Name: Txt("ride.class." + string(c)), Pct: pctOfF(s.KM, top),
			Value: TxtA("detail.travel.class_sum", "km", NumU(s.KM, 0, "km"), "n", s.Rides, "hours", Num(s.Minutes/minutesPerHourF, 1))})
	}
	return Block{Kind: BlockBars, Label: T("detail.travel.by_class"), Data: bars}
}

// minutesPerHourF converts ride minutes to hours.
const minutesPerHourF = 60.0

// travelYear: km per month and class this year (with last year's total
// from Dawarich's stats), and when rides happen.
func travelYear(travel metrics.Travel, data *sources.DawarichDataset, today time.Time) []Block {
	months := metrics.MonthKM(travel.Rides, today.Year())
	g := Graph{Kind: GraphLine, Mark: int(today.Month()) - 1, Ticks: []any{"01", "12"}}
	for _, c := range metrics.RideClasses {
		values := make([]float64, monthsPerYear)
		for m := range monthsPerYear {
			values[m] = math.Round(months[m][c])
		}
		if hasValues(values) {
			g.Series = append(g.Series, Series{Values: values, Class: classSeries[c], Label: Txt("ride.class." + string(c))})
		}
	}
	lastYear := make([]float64, monthsPerYear)
	for m := range monthsPerYear {
		lastYear[m] = math.Round(metrics.DawarichMonthKM(data.Stats, time.Date(today.Year()-1, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)))
	}
	if len(g.Series) > 0 && hasValues(lastYear) {
		g.Series = append(g.Series, Series{Values: lastYear, Class: "s4", Label: TxtA("detail.travel.last_year", "year", today.Year()-1)})
	}
	var out []Block
	if len(g.Series) > 0 {
		out = append(out, Block{Kind: BlockGraph, Label: T("detail.travel.by_month"), Meta: "km", Data: g})
	}

	year := travel.Between(time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC), today)
	heat := metrics.Heat(year)
	top := 0.0
	for _, day := range heat {
		for _, m := range day {
			top = max(top, m)
		}
	}
	if top > 0 {
		var shares [7][24]float64
		for d := range heat {
			for h := range heat[d] {
				// busyHeat reads Sunday first
				shares[(d+1)%weekDays][h] = heat[d][h] / top
			}
		}
		out = append(out, Block{Kind: BlockHeat, Label: T("detail.travel.when"), Data: busyHeat(shares)})
	}

	if weeks := metrics.Weeks(year); len(weeks) > 1 {
		values := make([]float64, len(weeks))
		for i, w := range weeks {
			for _, m := range w.Minutes {
				values[i] += m / minutesPerHourF
			}
			values[i] = math.Round(values[i]*10) / 10
		}
		out = append(out, Block{Kind: BlockGraph, Label: T("detail.travel.per_week"), Meta: "h",
			Data: Graph{Kind: GraphCols, Mark: -1, Series: []Series{{Values: values, Class: "s1"}}, Ticks: []any{Day(weeks[0].Week), Day(weeks[len(weeks)-1].Week)}}})
	}
	return out
}

// travelTables: business per customer (travel time against booked
// time), destinations, modes.
func travelTables(rides []metrics.ClassedRide, names map[int64]string, booked map[int64]float64, rate float64) []Block {
	var out []Block

	byCustomer := metrics.ByCustomer(rides)
	ids := make([]int64, 0, len(byCustomer))
	for id := range byCustomer {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return byCustomer[ids[i]].KM > byCustomer[ids[j]].KM })
	var rows [][]Cell
	for _, id := range ids[:min(len(ids), travelTop)] {
		s := byCustomer[id]
		name := names[id]
		if id == 0 || name == "" {
			name = "?"
		}
		share := any("–")
		if booked[id] > 0 {
			share = TxtA("detail.travel.pct", "pct", Num(math.Round(s.Minutes/booked[id]*percentScale), 0))
		}
		rows = append(rows, []Cell{{Value: name}, {Value: NumU(s.KM, 0, "km")}, {Value: NumU(s.Minutes/minutesPerHourF, 1, "h")}, {Value: share}, {Value: Money(s.PayKM*rate, "")}})
	}
	if len(rows) > 0 {
		out = append(out, Block{Kind: BlockTable, Label: T("detail.travel.by_customer"), Data: Table{
			Head: []Text{T("col.customer"), T("col.km"), T("detail.travel.hours"), T("detail.travel.of_booked"), T("col.amount")}, Rows: rows, Num: []int{1, 2, 3, 4}}})
	}

	rows = nil
	dests := metrics.Destinations(rides)
	for _, d := range dests[:min(len(dests), travelTop)] {
		name := Txt("detail.travel.unknown_place")
		if d.Site != nil {
			name = TxtA("detail.plain", "text", d.Site.Name)
		}
		rows = append(rows, []Cell{{Value: name}, {Value: d.Rides}, {Value: NumU(d.KM, 0, "km")}, {Value: Day(d.Last)}})
	}
	if len(rows) > 0 {
		out = append(out, Block{Kind: BlockTable, Label: T("detail.travel.destinations"), Data: Table{
			Head: []Text{T("detail.travel.place"), T("detail.travel.rides"), T("col.km"), T("detail.travel.last")}, Rows: rows, Num: []int{1, 2}}})
	}

	byMode := metrics.ByMode(rides)
	top := 0.0
	for _, s := range byMode {
		top = max(top, s.KM)
	}
	modes := make([]string, 0, len(byMode))
	for m := range byMode {
		modes = append(modes, m)
	}
	sort.Slice(modes, func(i, j int) bool { return byMode[modes[i]].KM > byMode[modes[j]].KM })
	var bars []ShareBar
	for _, m := range modes {
		bars = append(bars, ShareBar{Name: Txt("ride.mode." + m), Pct: pctOfF(byMode[m].KM, top), Value: NumU(byMode[m].KM, 0, "km")})
	}
	if len(bars) > 0 {
		out = append(out, Block{Kind: BlockBars, Label: T("detail.travel.by_mode"), Data: bars})
	}
	return out
}

// travelFacts: commute, days off, business car, fuel, places without a
// site.
func travelFacts(rides []metrics.ClassedRide, kimai *sources.KimaiDataset, set metrics.TravelSettings, results map[string]any, start time.Time) []Block {
	var rows [][]Cell
	weekend, absent := metrics.OffDays(rides, kimai)
	if weekend.Rides > 0 {
		rows = append(rows, []Cell{{Value: Txt("detail.travel.weekend")}, {Value: TxtA("detail.travel.off_value", "n", weekend.Rides, "km", NumU(weekend.KM, 0, "km"))}})
	}
	if absent.Rides > 0 {
		rows = append(rows, []Cell{{Value: Txt("detail.travel.absent")}, {Value: TxtA("detail.travel.off_value", "n", absent.Rides, "km", NumU(absent.KM, 0, "km"))}})
	}
	if set.Base == metrics.BaseWork {
		days, km := metrics.CommuteDays(rides)
		rows = append(rows, []Cell{{Value: Txt("detail.travel.commute_days")}, {Value: TxtA("detail.travel.commute_value", "days", days, "km", NumU(km, 1, "km"))}})
	}
	if share, km := metrics.CarShare(rides); km > 0 {
		state := ""
		if set.CompanyCar && share > metrics.CarShareLimit {
			state = "warn"
		}
		rows = append(rows, []Cell{{Value: Txt("detail.travel.car_private")}, {Value: TxtA("detail.travel.car_value", "share", Num(math.Round(share*percentScale), 0), "km", NumU(km, 0, "km")), State: state}})
	}
	if sure, ok := results[peerSure].(*sources.SureDataset); ok {
		if spent, km := metrics.FuelSpent(sure, set.FuelWords, start), carKM(rides); spent > 0 && km > 0 {
			rows = append(rows, []Cell{{Value: Txt("detail.travel.fuel")}, {Value: TxtA("detail.travel.fuel_value", "amount", Money(spent, sure.Currency), "per_km", Money(spent/km, sure.Currency))}})
		}
	}
	if n := len(metrics.Unplaced(rides, unplacedMin)); n > 0 {
		rows = append(rows, []Cell{{Value: Txt("detail.travel.unplaced")}, {Value: TxtA("detail.travel.unplaced_value", "n", n), State: "warn", Href: travelPlacesURL}})
	}
	if len(rows) == 0 {
		return nil
	}
	return []Block{{Kind: BlockTable, Label: T("detail.travel.facts"), Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: rows}}}
}

// travelPlacesURL opens the places tab of the space's Dawarich connection.
const travelPlacesURL = "/travel/places"

func carKM(rides []metrics.ClassedRide) float64 {
	_, km := metrics.CarShare(rides)
	return km
}
