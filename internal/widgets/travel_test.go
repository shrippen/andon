package widgets_test

import (
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
