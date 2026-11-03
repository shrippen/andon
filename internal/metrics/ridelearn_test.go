package metrics_test

import (
	"cmp"
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

// learnedTravel classifies geo's rides with a model taught options.
func learnedTravel(geo *sources.DawarichDataset, kimai *sources.KimaiDataset, taught, options map[string]any) []metrics.ClassedRide {
	m := bayes.New()
	metrics.TeachRides(m, geo, nil, taught)
	set := metrics.TravelSettingsOf(nil)
	set.Learned = bayes.Use{Model: m, MinP: learnedMinP}
	return metrics.TravelOf(geo, kimai, options, set, at("23:00")).Rides
}

// proposals are the rides' learned classes, "-" for none.
func proposals(rides []metrics.ClassedRide) []string {
	out := make([]string, len(rides))
	for i, r := range rides {
		out[i] = cmp.Or(string(r.Proposed), "-")
	}
	return out
}

// Home → office set business, home → lake private: the same rides are
// proposed so (private is the rest rule's class already), the walk
// ending at the office too; rides without a known start or end get
// nothing. A proposal counts in no sum.
func TestLearnedProposesForRest(t *testing.T) {
	geo, _ := testData()
	taught := byHand(metrics.Rides(geo), map[int]metrics.RideClass{0: metrics.ClassBusiness, 4: metrics.ClassPrivate})

	rides := learnedTravel(geo, nil, taught, nil)

	sameList(t, classes(rides), []string{"private/rest", "private/rest", "private/rest", "private/rest", "private/rest", "private/rest"})
	sameList(t, proposals(rides), []string{"business", "business", "-", "-", "-", "-"})
	if sum := metrics.ByClass(rides)[metrics.ClassBusiness]; sum.Rides != 0 {
		t.Fatalf("a proposal counts nowhere: %+v", sum)
	}
}

// A class set by hand and every rule win over the model.
func TestLearnedOnlyForRest(t *testing.T) {
	geo, kimai := testData()
	all := metrics.Rides(geo)
	// Every ride taught business: the model would propose it anywhere.
	taught := byHand(all, map[int]metrics.RideClass{0: metrics.ClassBusiness, 1: metrics.ClassBusiness, 2: metrics.ClassBusiness,
		3: metrics.ClassBusiness, 4: metrics.ClassBusiness, 5: metrics.ClassBusiness})

	rides := learnedTravel(geo, kimai, taught, byHand(all, map[int]metrics.RideClass{4: metrics.ClassPrivate}))

	// walk and rest: proposed; customer rides: rule; lake: by hand.
	sameList(t, classes(rides), []string{"private/rest", "private/rest", "business/customer", "business/customer", "private/manual", "private/rest"})
	sameList(t, proposals(rides), []string{"business", "business", "-", "-", "-", "business"})
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
