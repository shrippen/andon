package metrics_test

import (
	"strconv"
	"testing"

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
	metrics.TeachRides(m, geo, taught)
	set := metrics.TravelSettingsOf(nil)
	set.Learned = bayes.Use{Model: m, MinP: learnedMinP}
	return metrics.TravelOf(geo, kimai, options, set, at("23:00")).Rides
}

// Home → office set business, home → lake private: the same rides are
// proposed so, the walk ending at the office too; rides without a known
// start or end stay with the rest rule.
func TestLearnedClassesRestRides(t *testing.T) {
	geo, _ := testData()
	taught := byHand(metrics.Rides(geo), map[int]metrics.RideClass{0: metrics.ClassBusiness, 4: metrics.ClassPrivate})

	rides := learnedTravel(geo, nil, taught, nil)

	sameList(t, classes(rides), []string{"business/learned", "business/learned", "private/rest", "private/rest", "private/learned", "private/rest"})
}

// A class set by hand and every rule win over the model.
func TestLearnedOnlyForRest(t *testing.T) {
	geo, kimai := testData()
	all := metrics.Rides(geo)
	// Every ride taught business: the model would propose it anywhere.
	taught := byHand(all, map[int]metrics.RideClass{0: metrics.ClassBusiness, 1: metrics.ClassBusiness, 2: metrics.ClassBusiness,
		3: metrics.ClassBusiness, 4: metrics.ClassBusiness, 5: metrics.ClassBusiness})

	rides := learnedTravel(geo, kimai, taught, byHand(all, map[int]metrics.RideClass{4: metrics.ClassPrivate}))

	// walk: rest → learned; customer rides: rule; lake: by hand.
	sameList(t, classes(rides), []string{"business/learned", "business/learned", "business/customer", "business/customer", "private/manual", "business/learned"})
}

// A learned business ride bridges like any other: the ride between two
// learned customer rides becomes business by the chain rule.
func TestLearnedKeepsChain(t *testing.T) {
	geo, _ := testData()
	geo.Tracks[0].Segments = []sources.DawarichSegment{
		seg(home, customer, "08:00", "08:40", metrics.ModeDriving, 30),
		seg(customer, customer, "08:40", "12:00", metrics.ModeStationary, 0),
		seg(customer, lake, "12:00", "12:30", metrics.ModeDriving, 20),
		seg(lake, lake, "12:30", "13:00", metrics.ModeStationary, 0),
		seg(lake, customer, "13:00", "13:30", metrics.ModeDriving, 20),
	}
	geo.Tracks[0].Segments[2].FromLat, geo.Tracks[0].Segments[2].FromLon = office[0]+0.05, office[1] // leaves from nowhere known
	taught := byHand(metrics.Rides(geo), map[int]metrics.RideClass{0: metrics.ClassBusiness})

	// No Kimai: the customer is no site, only its cell is known.
	rides := learnedTravel(geo, nil, taught, nil)

	sameList(t, classes(rides), []string{"business/learned", "business/chain", "business/learned"})
}

// Handed back to the rules, a ride is forgotten; a model without a
// known place proposes nothing.
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
	if metrics.TeachRides(m, nil, nil) {
		t.Fatal("no data, nothing learned")
	}
}
