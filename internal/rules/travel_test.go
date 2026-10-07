package rules_test

import (
	"andon/internal/caps"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// A September of rides, reported on 3 October: home (52.52, 13.40) to
// client Acme (customer 5, its Dawarich area mapped by the mileage
// plugin) and back on 10 September, a private ride to the lake on 12th.
func travelEnv(settings map[string]any) rules.Env {
	at := func(day, hhmm string) int64 {
		t, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, time.Local)
		return t.Unix()
	}
	seg := func(day, a, b string, from, to [2]float64, mode string, km float64) sources.DawarichSegment {
		return sources.DawarichSegment{Start: at(day, a), End: at(day, b), Mode: mode, Meters: km * 1000, FromLat: from[0], FromLon: from[1], ToLat: to[0], ToLon: to[1]}
	}
	home, acme, lake := [2]float64{52.52, 13.40}, [2]float64{52.40, 13.06}, [2]float64{52.52, 13.52}
	geo := &sources.DawarichDataset{URL: "https://dawarich.example", TracksState: sources.TracksOK,
		Areas: []sources.DawarichArea{{ID: 1, Name: "Zuhause", Lat: home[0], Lon: home[1], Radius: 100}, {ID: 2, Name: "Acme", Lat: acme[0], Lon: acme[1], Radius: 100}},
		Tracks: []sources.DawarichTrack{
			{ID: 1, Segments: []sources.DawarichSegment{
				seg("2026-09-10", "07:30", "08:15", home, acme, "driving", 30),
				seg("2026-09-10", "08:15", "17:00", acme, acme, "stationary", 0),
				seg("2026-09-10", "17:00", "17:45", acme, home, "driving", 30)}},
			{ID: 2, Segments: []sources.DawarichSegment{seg("2026-09-12", "10:00", "10:40", home, lake, "driving", 40)}},
		}}
	kimai := &sources.KimaiDataset{URL: "https://kimai.example", Caps: caps.Full(caps.HolderOf(enums.ServiceKimai)),
		Customers:  []sources.KimaiCustomer{{ID: 5, Name: "Acme GmbH"}},
		Places:     []sources.KimaiPlace{{ID: 1, AreaID: 1, Type: "home"}, {ID: 2, AreaID: 2, Type: "customer", CustomerID: 5}},
		Timesheets: []sources.KimaiSheet{{Begin: "2026-09-10T09:00:00+02:00", End: "2026-09-10T16:00:00+02:00", CustomerID: 5}}}
	ninja := &sources.NinjaDataset{Currency: "EUR", Clients: []sources.NinjaClient{{ID: 1, Name: "Acme GmbH"}}}
	if settings == nil {
		settings = map[string]any{}
	}
	return rules.Env{Today: day("2026-10-03"), Settings: settings,
		Datasets: map[string]any{"dawarich": geo, "kimai": kimai, "invoiceninja": ninja}}
}

func TestTravelCostsCountBusinessRides(t *testing.T) {
	found := run(t, "geo.travel_costs", nil, travelEnv(nil))
	if len(found) != 1 || found[0].Params["trips"] != 2 {
		t.Fatalf("got %+v", found)
	}
}

func TestPerDiemFromAbsence(t *testing.T) {
	// home 07:30 → home 17:45: over 8 h with business rides
	found := run(t, "geo.per_diem", nil, travelEnv(nil))
	if len(found) != 1 || found[0].Params["days"] != 1 || found[0].Params["full"] != 0 {
		t.Fatalf("got %+v", found)
	}
}

func TestTravelUnbilled(t *testing.T) {
	env := travelEnv(nil)
	found := run(t, "geo.travel_unbilled", nil, env)
	if len(found) != 1 || found[0].Params["customer"] != "Acme GmbH" {
		t.Fatalf("got %+v", found)
	}

	// an invoice with a travel line clears it
	ninja := env.Datasets["invoiceninja"].(*sources.NinjaDataset)
	ninja.Invoices = []sources.NinjaInvoice{{ClientID: 1, Date: "2026-10-01", Items: []string{"fahrtkosten september 60 km"}}}
	if found := run(t, "geo.travel_unbilled", nil, env); len(found) != 0 {
		t.Fatalf("billed: %+v", found)
	}
}

func TestPluginMissing(t *testing.T) {
	env := travelEnv(nil)
	if found := run(t, "geo.plugin_missing", nil, env); len(found) != 0 {
		t.Fatalf("plugin not in use: %+v", found)
	}
	kimai := env.Datasets["kimai"].(*sources.KimaiDataset)
	kimai.MileageTrips = []sources.KimaiMileageTrip{{Date: "2026-09-02", Purpose: "business"}}
	found := run(t, "geo.plugin_missing", nil, env)
	if len(found) != 1 || found[0].Params["count"] != 2 {
		t.Fatalf("got %+v", found)
	}
}

func TestCarPrivateShare(t *testing.T) {
	set := map[string]any{"travel": map[string]any{"company_car": true}, "rules": map[string]any{"geo.car_private_share": map[string]any{"min_km": 50.0}}}
	env := travelEnv(set)
	env.Today = day("2026-10-20")
	// 40 of 100 car km private: fine
	if found := run(t, "geo.car_private_share", nil, env); len(found) != 0 {
		t.Fatalf("40 %%: %+v", found)
	}
	geo := env.Datasets["dawarich"].(*sources.DawarichDataset)
	geo.Tracks[1].Segments[0].Meters = 90000
	found := run(t, "geo.car_private_share", nil, env)
	if len(found) != 1 || found[0].Severity != enums.SeverityWarn {
		t.Fatalf("60 %%: %+v", found)
	}
}

func TestUnplacedAndTracksMissing(t *testing.T) {
	env := travelEnv(map[string]any{"rules": map[string]any{"geo.unplaced": map[string]any{"rides": 1.0}}})
	env.Today = day("2026-09-20")
	found := run(t, "geo.unplaced", nil, env)
	if len(found) != 1 || found[0].Params["count"] != 1 || found[0].ActionURL != "/travel/places" {
		t.Fatalf("the lake: %+v", found)
	}

	geo := env.Datasets["dawarich"].(*sources.DawarichDataset)
	if found := run(t, "geo.tracks_missing", geo, env); len(found) != 0 {
		t.Fatalf("tracks are there: %+v", found)
	}
	geo.TracksState = sources.TracksMissing
	if found := run(t, "geo.tracks_missing", geo, env); len(found) != 1 || found[0].Message != "geo.tracks_missing" {
		t.Fatalf("got %+v", found)
	}
}

// TestChargeBusiness: the car charged on 9 September (6 €), then drove
// only to Acme and back; the charge of 11 September (4 €) went into the
// private ride to the lake. Reported in the first days of October.
func TestChargeBusiness(t *testing.T) {
	env := travelEnv(nil)
	session := func(day string, price float64) sources.EVCCSession {
		at := day + "T20:00:00Z"
		start, _ := time.Parse(time.RFC3339, at)
		return sources.EVCCSession{Loadpoint: "Carport", Created: start, Finished: start.Add(3 * time.Hour), KWh: price / 0.3, Price: price}
	}
	env.Datasets["evcc"] = &sources.EVCCDataset{Sessions: []sources.EVCCSession{session("2026-09-11", 4), session("2026-09-09", 6)}}

	found := run(t, "cross.charge_business", nil, env)
	if len(found) != 1 || found[0].Params["sessions"] != 2 || found[0].Params["month"] != "09/2026" {
		t.Fatalf("got %+v", found)
	}
	if amount := found[0].Params["amount"].(map[string]any); amount["$money"] != 6.0 {
		t.Fatalf("amount %+v", amount)
	}

	env.Today = day("2026-10-20") // reported in the first days only
	if found := run(t, "cross.charge_business", nil, env); len(found) != 0 {
		t.Fatalf("late: %+v", found)
	}
}

// TestChargeBusinessDemo: Mara's wallbox sessions and her rides of last
// month give a business share.
func TestChargeBusinessDemo(t *testing.T) {
	now := time.Now().UTC()
	env := todayEnv(nil)
	env.Today = time.Date(now.Year(), now.Month(), 3, 0, 0, 0, 0, time.UTC)
	env.Datasets = map[string]any{"dawarich": sources.DemoDawarich(now), "kimai": sources.DemoKimai(now), "evcc": sources.DemoEVCC(now)}
	found := run(t, "cross.charge_business", nil, env)
	if len(found) != 1 || found[0].Params["amount"].(map[string]any)["$money"].(float64) <= 0 {
		t.Fatalf("demo %+v", found)
	}
}
