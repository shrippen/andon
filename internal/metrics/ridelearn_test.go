package metrics_test

import (
	"strconv"
	"testing"
	"time"

	"andon/internal/bayes"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// learnedMinP is the threshold of the tests (the instance's default).
const learnedMinP = 0.6

// byHand is the rides option setting rides (by index) to classes.
func byHand(rides []metrics.Ride, set map[int]metrics.RideClass) map[string]any {
	all := map[string]any{}
	for i, c := range set {
		all[strconv.FormatInt(rides[i].Start.Unix(), 10)] = string(c)
	}
	return map[string]any{metrics.OptionRides: all}
}

// suggested is what a model taught options suggests for each of geo's
// rides, classified with options, "-" for nothing.
func suggested(geo *sources.DawarichDataset, kimai *sources.KimaiDataset, taught, options map[string]any) ([]metrics.ClassedRide, []string) {
	m := bayes.New()
	metrics.TeachRides(m, geo, nil, taught)
	u := bayes.Use{Model: m, MinP: learnedMinP}
	rides := metrics.TravelOf(geo, kimai, options, metrics.TravelSettingsOf(nil), at("23:00")).Rides
	out := make([]string, len(rides))
	for i, r := range rides {
		out[i] = "-"
		if s := metrics.SuggestRide(u, rides, r, 1); len(s) > 0 {
			out[i] = string(s[0])
		}
	}
	return rides, out
}

// Home → office set business, home → lake private: the same rides get
// those suggestions, the walk ending at the office too; rides without a
// known start or end get none. The rides keep the rules' class.
func TestSuggestRideForRest(t *testing.T) {
	geo, _ := testData()
	taught := byHand(metrics.Rides(geo), map[int]metrics.RideClass{0: metrics.ClassBusiness, 4: metrics.ClassPrivate})

	rides, got := suggested(geo, nil, taught, nil)

	sameList(t, got, []string{"business", "business", "-", "-", "private", "-"})
	sameList(t, classes(rides), []string{"private/rest", "private/rest", "private/rest", "private/rest", "private/rest", "private/rest"})
}

// A class set by hand and every rule get no suggestion.
func TestSuggestRideOnlyForRest(t *testing.T) {
	geo, kimai := testData()
	all := metrics.Rides(geo)
	// Every ride taught business: the model would suggest it anywhere.
	taught := byHand(all, map[int]metrics.RideClass{0: metrics.ClassBusiness, 1: metrics.ClassBusiness, 2: metrics.ClassBusiness,
		3: metrics.ClassBusiness, 4: metrics.ClassBusiness, 5: metrics.ClassBusiness})

	_, got := suggested(geo, kimai, taught, byHand(all, map[int]metrics.RideClass{4: metrics.ClassPrivate}))

	// walk and rest: suggested; customer rides: rule; lake: by hand.
	sameList(t, got, []string{"business", "business", "-", "-", "-", "business"})
}

// The plugin's mileage trips are classes set by hand too.
func TestTeachRidesPluginTrips(t *testing.T) {
	geo, kimai := testData()
	kimai.MileageTrips = []sources.KimaiMileageTrip{{ID: 9, Departure: at("18:00").Format(time.RFC3339), Arrival: at("18:40").Format(time.RFC3339), Purpose: "business"}}
	m := bayes.New()

	if !metrics.TeachRides(m, geo, kimai, nil) || m.Stats().Examples != 1 {
		t.Fatalf("plugin trip learned: %+v", m.Stats())
	}
	if metrics.TeachRides(m, geo, nil, nil) {
		t.Fatal("without Kimai the trip stays learned, unchanged")
	}
}

// Handed back to the rules, a ride is forgotten.
func TestLearnRideForgets(t *testing.T) {
	geo, _ := testData()
	m := bayes.New()
	ride := metrics.TravelOf(geo, nil, nil, metrics.TravelSettingsOf(nil), at("23:00")).Rides[0]

	if !metrics.LearnRide(m, ride, metrics.ClassBusiness) || m.Stats().Examples != 1 {
		t.Fatalf("learned: %+v", m.Stats())
	}
	if metrics.LearnRide(m, ride, metrics.ClassBusiness) {
		t.Fatal("the same class again changes nothing")
	}
	if !metrics.LearnRide(m, ride, metrics.ClassAuto) || m.Stats().Examples != 0 {
		t.Fatalf("forgotten: %+v", m.Stats())
	}
	if metrics.TeachRides(m, nil, nil, nil) {
		t.Fatal("no data, nothing learned")
	}
}
