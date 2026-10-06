package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Places of the test: home, office, a customer, a lake.
var (
	home     = [2]float64{52.52, 13.40}
	office   = [2]float64{52.50, 13.30}
	customer = [2]float64{52.40, 13.06}
	lake     = [2]float64{52.52, 13.52}
)

// at is a time on 5 Jan 2026 (a Monday), local.
func at(hhmm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", "2026-01-05 "+hhmm, time.Local)
	return t
}

func seg(from, to [2]float64, a, b, mode string, km float64) sources.DawarichSegment {
	return sources.DawarichSegment{Start: at(a).Unix(), End: at(b).Unix(), Mode: mode, Meters: km * 1000,
		FromLat: from[0], FromLon: from[1], ToLat: to[0], ToLon: to[1]}
}

// trackedDay is one tracked day: drive to the office, walk, drive to the
// customer and home, cycle to the lake and back.
func trackedDay() *sources.DawarichDataset {
	return &sources.DawarichDataset{TracksState: sources.TracksOK, Tracks: []sources.DawarichTrack{{ID: 1, Segments: []sources.DawarichSegment{
		seg(home, home, "06:00", "07:30", metrics.ModeStationary, 0),
		seg(home, office, "07:30", "07:40", metrics.ModeDriving, 4),
		seg(office, office, "07:40", "07:42", metrics.ModeStationary, 0), // traffic light
		seg(office, office, "07:42", "07:50", metrics.ModeDriving, 3),
		seg(office, office, "07:50", "10:00", metrics.ModeStationary, 0),
		seg(office, office, "10:00", "10:20", metrics.ModeWalking, 1.5),
		seg(office, office, "10:20", "12:00", metrics.ModeStationary, 0),
		seg(office, customer, "12:00", "12:35", metrics.ModeDriving, 25),
		seg(customer, customer, "12:35", "16:00", metrics.ModeStationary, 0),
		seg(customer, home, "16:00", "16:45", metrics.ModeDriving, 30),
		seg(home, home, "16:45", "18:00", metrics.ModeStationary, 0),
		seg(home, lake, "18:00", "18:40", metrics.ModeCycling, 9),
		seg(lake, lake, "18:40", "19:10", metrics.ModeStationary, 0),
		seg(lake, home, "19:10", "19:50", metrics.ModeCycling, 9),
	}}}}
}

func TestRidesSplitAtStopsAndModes(t *testing.T) {
	rides := metrics.Rides(trackedDay())

	want := []struct {
		km   float64
		mode string
	}{{7, "driving"}, {1.5, "walking"}, {25, "driving"}, {30, "driving"}, {9, "cycling"}, {9, "cycling"}}
	if len(rides) != len(want) {
		t.Fatalf("got %d rides: %+v", len(rides), rides)
	}
	for i, w := range want {
		if rides[i].KM != w.km || rides[i].Mode != w.mode {
			t.Fatalf("ride %d: got %.1f %s, want %.1f %s", i, rides[i].KM, rides[i].Mode, w.km, w.mode)
		}
	}
	if !rides[0].Start.Equal(at("07:30")) || !rides[0].End.Equal(at("07:50")) {
		t.Fatalf("short stop must stay in the ride: %v–%v", rides[0].Start, rides[0].End)
	}
}

// book: Dawarich areas home and office, the customer from the Kimai
// plugin (no Dawarich link).
func testData() (*sources.DawarichDataset, *sources.KimaiDataset) {
	geo := trackedDay()
	geo.Areas = []sources.DawarichArea{{ID: 1, Name: "Zuhause", Lat: home[0], Lon: home[1], Radius: 150}, {ID: 2, Name: "Büro", Lat: office[0], Lon: office[1], Radius: 150}}
	kimai := &sources.KimaiDataset{Places: []sources.KimaiPlace{
		{ID: 1, AreaID: 1, Type: "home"},
		{ID: 2, AreaID: 2, Type: "work"},
		{ID: 3, Name: "Kunde Potsdam", Type: "customer", CustomerID: 12, Lat: customer[0], Lon: customer[1], Radius: 150},
	}}
	return geo, kimai
}

func TestBookMergesPluginAndAndon(t *testing.T) {
	geo, kimai := testData()
	options := map[string]any{"places": map[string]any{"area:2": map[string]any{"kind": "customer", "customer_id": 30.0}}}

	book := metrics.BookOf(geo, kimai, options)

	if s := book.Site("area:1"); s.Kind != metrics.KindHome || s.Origin != metrics.OriginPlugin || s.PluginID != 1 {
		t.Fatalf("home: %+v", s)
	}
	if s := book.Site("area:2"); s.Kind != metrics.KindCustomer || s.CustomerID != 30 || s.Origin != metrics.OriginAndon {
		t.Fatalf("Andon must override the plugin: %+v", s)
	}
	if s := book.At(customer[0]+0.0005, customer[1]); s == nil || s.Key != "kimai:3" || s.CustomerID != 12 {
		t.Fatalf("plugin place without Dawarich link: %+v", s)
	}
	if book.At(lake[0], lake[1]) != nil {
		t.Fatal("no site at the lake")
	}
}

func classes(rides []metrics.ClassedRide) []string {
	out := make([]string, len(rides))
	for i, r := range rides {
		out[i] = string(r.Class) + "/" + string(r.Reason)
	}
	return out
}

func sameList(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestClassifyRules(t *testing.T) {
	geo, kimai := testData()
	// booked 07:45–12:00 for customer 5: covers the walk, not half of the
	// first drive (07:30–07:50)
	kimai.Timesheets = []sources.KimaiSheet{{Begin: at("07:45").Format(time.RFC3339), End: at("12:00").Format(time.RFC3339), CustomerID: 5}}
	book := metrics.BookOf(geo, kimai, nil)

	rides := metrics.Classify(metrics.Rides(geo), book, kimai, metrics.BaseWork, at("23:00"))

	sameList(t, classes(rides), []string{"commute/commute", "business/kimai", "business/customer", "business/customer", "private/rest", "private/rest"})
	if sum := metrics.ByClass(rides)[metrics.ClassBusiness]; sum.KM != 56.5 || sum.PayKM != 55 {
		t.Fatalf("the walk earns no km rate: %+v", sum)
	}
	if rides[1].CustomerID != 5 || rides[2].CustomerID != 12 {
		t.Fatalf("customers: %d %d", rides[1].CustomerID, rides[2].CustomerID)
	}
	if !rides[2].Unconfirmed {
		t.Fatal("customer 12 has no time that day: unconfirmed")
	}

	// With home as the place of business there is no commute.
	rides = metrics.Classify(metrics.Rides(geo), book, kimai, metrics.BaseHome, at("23:00"))
	if rides[0].Class != metrics.ClassPrivate {
		t.Fatalf("no commute from home: %s", rides[0].Class)
	}
}

func TestClassifyHalfInBookedTime(t *testing.T) {
	geo, kimai := testData()
	// 07:41–08:00 covers 9 of the first drive's 20 minutes: not enough
	kimai.Timesheets = []sources.KimaiSheet{{Begin: at("07:41").Format(time.RFC3339), End: at("08:00").Format(time.RFC3339), CustomerID: 5}}
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), kimai, metrics.BaseHome, at("23:00"))
	if rides[0].Reason == metrics.ReasonKimai {
		t.Fatal("less than half booked")
	}

	// 07:39–08:00 covers 11 of 20
	kimai.Timesheets[0].Begin = at("07:39").Format(time.RFC3339)
	rides = metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), kimai, metrics.BaseHome, at("23:00"))
	if rides[0].Reason != metrics.ReasonKimai {
		t.Fatalf("half booked: %s", rides[0].Reason)
	}
}

func TestClassifyPluginWins(t *testing.T) {
	geo, kimai := testData()
	kimai.Projects = []sources.KimaiProject{{ID: 4, CustomerID: 12}}
	kimai.MileageTrips = []sources.KimaiMileageTrip{{Departure: at("18:00").Format(time.RFC3339), Arrival: at("18:40").Format(time.RFC3339), Purpose: "business", Project: 4}}
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), kimai, metrics.BaseHome, at("23:00"))
	if r := rides[4]; r.Class != metrics.ClassBusiness || r.Reason != metrics.ReasonPlugin || r.CustomerID != 12 {
		t.Fatalf("plugin trip: %+v", r)
	}
}

func TestClassifyChain(t *testing.T) {
	geo, kimai := testData()
	// office → shop (walk) between two customer rides becomes business
	geo.Tracks[0].Segments = []sources.DawarichSegment{
		seg(home, customer, "08:00", "08:40", metrics.ModeDriving, 30),
		seg(customer, customer, "08:40", "12:00", metrics.ModeStationary, 0),
		seg(customer, lake, "12:00", "12:30", metrics.ModeDriving, 20),
		seg(lake, lake, "12:30", "13:00", metrics.ModeStationary, 0),
		seg(lake, customer, "13:00", "13:30", metrics.ModeDriving, 20),
	}
	geo.Tracks[0].Segments[2].FromLat, geo.Tracks[0].Segments[2].FromLon = office[0]+0.05, office[1] // leaves from nowhere known
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), nil, metrics.BaseHome, at("23:00"))
	sameList(t, classes(rides), []string{"business/customer", "business/chain", "business/customer"})
}

func TestAllowances(t *testing.T) {
	geo, kimai := testData()
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), nil, metrics.BaseHome, at("23:00"))

	// home 07:30 → home 16:45 with business rides: 9 h 15 min
	days := metrics.Allowances(rides, metrics.BaseHome)
	if len(days) != 1 || days[0].Full || days[0].Amount() != metrics.AllowancePartial {
		t.Fatalf("got %+v", days)
	}
}

func TestDestinationsAndUnplaced(t *testing.T) {
	geo, kimai := testData()
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), nil, metrics.BaseHome, at("23:00"))

	unplaced := metrics.Unplaced(rides, 1)
	if len(unplaced) != 1 || unplaced[0].Site != nil || unplaced[0].Lat != lake[0] {
		t.Fatalf("the lake has no site: %+v", unplaced)
	}
	byClass := metrics.ByClass(rides)
	if byClass[metrics.ClassPrivate].KM != 26.5 || byClass[metrics.ClassBusiness].KM != 55 {
		t.Fatalf("km: %+v", byClass)
	}
}

func TestEstimatedRidesWithoutTracks(t *testing.T) {
	geo, kimai := testData()
	geo.TracksState, geo.Tracks = sources.TracksMissing, nil
	lat, lon := customer[0], customer[1]
	geo.Visits = []sources.DawarichVisit{{Start: at("09:00").Format(time.RFC3339), End: at("15:00").Format(time.RFC3339), Lat: &lat, Lon: &lon}}

	travel := metrics.TravelOf(geo, kimai, nil, metrics.TravelSettings{Base: metrics.BaseHome}, at("23:00"))

	if !travel.Estimated || len(travel.Rides) != 2 || travel.Rides[0].Class != metrics.ClassBusiness {
		t.Fatalf("got %+v", travel)
	}
}

func TestOffDaysAndBookedMinutes(t *testing.T) {
	geo, kimai := testData()
	kimai.Absences = []sources.KimaiAbsence{{Start: "2026-01-05", End: "2026-01-05", Status: "approved"}}
	kimai.Timesheets = []sources.KimaiSheet{{Begin: "2026-01-05T09:00:00+01:00", Minutes: 120, CustomerID: 12}}
	rides := metrics.Classify(metrics.Rides(geo), metrics.BookOf(geo, kimai, nil), nil, metrics.BaseHome, at("23:00"))

	weekend, absent := metrics.OffDays(rides, kimai)
	if weekend.Rides != 0 || absent.KM != 26.5 {
		t.Fatalf("a Monday off: weekend %+v, absent %+v", weekend, absent)
	}
	if m := metrics.BookedMinutes(kimai, day("2026-01-01"), day("2026-01-31")); m[12] != 120 {
		t.Fatalf("booked: %v", m)
	}
}
