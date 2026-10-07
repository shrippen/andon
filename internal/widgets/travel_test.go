package widgets_test

import (
	"strings"

	"andon/internal/i18n"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// ride is a track of one segment on day at 09:00 (local), from home (52.52,
// 13.40) to lat/lon.
func ride(day string, lat, lon, km float64, mode string) sources.DawarichTrack {
	start, _ := time.ParseInLocation("2006-01-02 15:04", day+" 09:00", time.Local)
	return sources.DawarichTrack{Start: start.Unix(), End: start.Add(30 * time.Minute).Unix(), Segments: []sources.DawarichSegment{{
		Start: start.Unix(), End: start.Add(30 * time.Minute).Unix(), Mode: mode, Meters: km * 1000,
		FromLat: 52.52, FromLon: 13.40, ToLat: lat, ToLon: lon}}}
}

// travelData: home and a client area (the Kimai mileage plugin maps it to
// customer 5); a business and a private ride in September, a private
// one in August.
func travelData() map[string]any {
	geo := &sources.DawarichDataset{TracksState: sources.TracksOK,
		Areas:  []sources.DawarichArea{{ID: 1, Name: "Zuhause", Lat: 52.52, Lon: 13.40, Radius: 100}, {ID: 2, Name: "Acme", Lat: 52.40, Lon: 13.06, Radius: 100}},
		Tracks: []sources.DawarichTrack{ride("2026-08-20", 52.60, 13.50, 30, "driving"), ride("2026-09-10", 52.40, 13.06, 20, "driving"), ride("2026-09-12", 52.52, 13.52, 8, "cycling")},
		Stats:  map[string]any{"yearlyStats": []any{map[string]any{"year": 2026.0, "totalCountriesVisited": 2.0, "totalCitiesVisited": 14.0}}}}
	kimai := &sources.KimaiDataset{Places: []sources.KimaiPlace{{ID: 1, AreaID: 1, Type: "home"}, {ID: 2, AreaID: 2, Type: "customer", CustomerID: 5}}}
	return map[string]any{"data": geo, "kimai": kimai}
}

// TestTravelTile: this month's rides, business against private, against
// last month; the business share as bar.
func TestTravelTile(t *testing.T) {
	v := viewOf(t, "travel", map[string]any{}, travelData(), enums.ServiceDawarich, nil)
	if v["HeadKM"] != 28.0 || v["BusinessKM"] != 20.0 || v["PrivateKM"] != 8.0 || v["PrevKM"] != 30.0 {
		t.Fatalf("view: %+v", v)
	}
	if v["Trips"] != 1 || v["TripAmount"] != 6.0 || v["Bar"] != 71 || v["Cities"] != 14 {
		t.Fatalf("view: %+v", v)
	}
}

// TestTravelYear: the year, and no bar when asked.
func TestTravelYear(t *testing.T) {
	v := viewOf(t, "travel", map[string]any{"period": "year"}, travelData(), enums.ServiceDawarich, nil)
	if v["HeadKM"] != 58.0 || v["Year"] != true {
		t.Fatalf("year: %+v", v)
	}
	v = viewOf(t, "travel", map[string]any{"hide_bar": true}, travelData(), enums.ServiceDawarich, nil)
	if v["Bar"] != nil {
		t.Fatalf("month without bar: %+v", v)
	}
}

// TestTravelDetail: the dialog lists this month's rides, newest first.
func TestTravelDetail(t *testing.T) {
	kind, _ := widgets.Get("travel")
	cfg, _ := widgets.Decode("travel", map[string]any{})
	d := kind.Detail(cfg, travelData(), ctxFor(enums.ServiceDawarich, nil))
	body, ok := d.Body.(*widgets.DetailBody)
	if !ok || body.List == nil || len(body.List.Items) != 2 || body.List.Items[0].State != "off" {
		t.Fatalf("detail: %+v", d.Body)
	}
}

// TestTravelRoute: the dialog reads the shown ride's points and draws
// them between start and end; points outside the ride are left out.
func TestTravelRoute(t *testing.T) {
	kind, _ := widgets.Get("travel")
	cfg, _ := widgets.Decode("travel", map[string]any{})
	ctx := ctxFor(enums.ServiceDawarich, nil)
	results := travelData()

	qs := kind.PickQueries(cfg, results, ctx)
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-12 09:00", time.Local)
	if len(qs) != 1 || qs[0].Source != "dawarich.route" || qs[0].Params["from"] != start.UTC().Format(time.RFC3339) {
		t.Fatalf("queries: %+v", qs)
	}

	results[qs[0].Name] = &sources.DawarichRoute{Points: []sources.RoutePoint{
		{Lat: 52.50, Lon: 13.45, At: start.Add(10 * time.Minute)}, {Lat: 1, Lon: 1, At: start.Add(2 * time.Hour)}}}
	body := kind.Detail(cfg, results, ctx).Body.(*widgets.DetailBody)
	for _, b := range body.Blocks {
		if m, ok := b.Data.(*widgets.MapData); ok && b.Kind == widgets.BlockMap {
			if len(m.Route) != 3 || m.Route[1] != [2]float64{13.45, 52.50} {
				t.Fatalf("route: %+v", m.Route)
			}
			return
		}
	}
	t.Fatal("no map")
}

// TestTravelDetailReading: while tracks are read, the dialog shows how
// far; admins get a link to the maintenance page.
func TestTravelDetailReading(t *testing.T) {
	kind, _ := widgets.Get("travel")
	cfg, _ := widgets.Decode("travel", map[string]any{})
	results := travelData()
	geo := results["data"].(*sources.DawarichDataset)
	geo.TracksState, geo.TracksRead, geo.TracksTotal = sources.TracksPartial, 300, 1200

	for _, admin := range []bool{false, true} {
		ctx := ctxFor(enums.ServiceDawarich, nil)
		ctx.Admin = admin
		body := kind.Detail(cfg, results, ctx).Body.(*widgets.DetailBody)
		var tasks *widgets.Tasks
		for _, b := range body.Blocks {
			if b.Kind == widgets.BlockTasks {
				tt := b.Data.(widgets.Tasks)
				tasks = &tt
			}
		}
		if tasks == nil || tasks.Done != 300 || tasks.Total != 1200 {
			t.Fatalf("admin %v: no reading progress: %+v", admin, tasks)
		}
		if linked := len(tasks.Items) == 1 && tasks.Items[0].Href == "/admin/operations#tasks"; linked != admin {
			t.Fatalf("admin %v: items %+v", admin, tasks.Items)
		}
	}
}

// TestTravelAmountFormula: the mileage names the km it pays: a bike ride
// to the client is business but earns no km rate, so 20 of 27 business
// km at 0.30 €/km make 6 €, and the tile says "20 km × 0.30 €/km".
func TestTravelAmountFormula(t *testing.T) {
	data := travelData()
	geo := data["data"].(*sources.DawarichDataset)
	geo.Tracks = append(geo.Tracks, ride("2026-09-11", 52.40, 13.06, 7, "cycling"))
	v := viewOf(t, "travel", map[string]any{"km_rate": 0.3}, data, enums.ServiceDawarich, nil)
	if v["BusinessKM"] != 27.0 || v["PayKM"] != 20.0 || v["TripAmount"] != 6.0 {
		t.Fatalf("view: %+v", v)
	}
	got := i18n.T("travel.amount", "de", map[string]any{"km": "20", "rate": "0,30", "amount": "6,00 €"})
	if !strings.Contains(got, "20 km × 0,30 €/km") {
		t.Fatalf("formula: %q", got)
	}
}
