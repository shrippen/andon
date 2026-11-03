package metrics

// Learned ride classes: where the rules fall through to the rest rule,
// the class a naive Bayes model learned from the rides set by hand.
//
//	rides set by hand ──► features ──► bayes.Model (services/learn, per Dawarich)
//	  from:<place>  to:<place>             │
//	  day:<weekday> hour:<3-hour block>    ▼
//	  km:<log₂ km>              rest ride ──► Best ≥ MinP ──► class, reason learned
//
// A place is the Dawarich area or place a ride starts or ends at, else
// its grid cell (about 1 km): "to:area:12", "to:c:5240,1306". Without a
// known place nothing is proposed: time and distance alone are no
// evidence. Manual classes and every rule win; the chain rule runs again
// afterwards, so a learned business ride bridges like any other.

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// ReasonLearned: the model learned the class from rides set by hand.
const ReasonLearned RideReason = "learned"

// Feature prefixes of a ride.
const (
	fromFeature = "from:"
	toFeature   = "to:"
	dayFeature  = "day:"
	hourFeature = "hour:"
	kmFeature   = "km:"
)

const (
	// cellDeg is a place's grid cell without a site, degrees (~1 km).
	cellDeg = 0.01
	// hourBlock is the hours one time-of-day feature spans.
	hourBlock = 3
)

// isPlaceFeature says whether f is a start or end: the evidence of a guess.
func isPlaceFeature(f string) bool {
	return strings.HasPrefix(f, fromFeature) || strings.HasPrefix(f, toFeature)
}

// TeachRides teaches m the classes set by hand (OptionRides) of geo's
// rides; a ride gone from geo stays learned. It says whether m changed.
// The plugin's trips are not read here: SetClass teaches those.
func TeachRides(m *bayes.Model, geo *sources.DawarichDataset, options map[string]any) bool {
	if geo == nil {
		return false
	}
	changed := false
	for _, r := range TravelOf(geo, nil, options, TravelSettingsOf(nil), time.Time{}).Rides {
		if r.Reason != ReasonManual {
			continue
		}
		changed = LearnRide(m, r, r.Class) || changed
	}
	return changed
}

// LearnRide teaches m that r is class; ClassAuto forgets r (handed back
// to the rules). It says whether m changed.
func LearnRide(m *bayes.Model, r ClassedRide, class RideClass) bool {
	id := strconv.FormatInt(r.Start.Unix(), 10)
	if class == ClassAuto {
		return m.Forget(id)
	}
	return m.Learn(id, string(class), rideFeatures(r))
}

// learned gives the rides the rules left to the rest rule the class u
// learned; u learns the rides set by hand here first, in memory.
func learned(rides []ClassedRide, u bayes.Use) {
	if !u.On() {
		return
	}
	for _, r := range rides {
		if r.Reason == ReasonManual {
			LearnRide(u.Model, r, r.Class)
		}
	}

	for i := range rides {
		r := &rides[i]
		if r.Reason != ReasonRest {
			continue
		}
		g, ok := u.Best(rideFeatures(*r), isPlaceFeature)
		if !ok || !slices.Contains(RideClasses, RideClass(g.Label)) {
			continue
		}
		r.Class, r.Reason = RideClass(g.Label), ReasonLearned
	}
	chain(rides)
}

// rideFeatures are r's start, end, weekday, time of day and distance.
func rideFeatures(r ClassedRide) []string {
	start := r.Start.In(time.Local)
	return []string{
		fromFeature + placeOf(r.From, r.FromLat, r.FromLon),
		toFeature + placeOf(r.To, r.ToLat, r.ToLon),
		dayFeature + strconv.Itoa(int(start.Weekday())),
		hourFeature + strconv.Itoa(start.Hour()/hourBlock),
		kmFeature + strconv.Itoa(int(math.Log2(max(r.KM, 1)))),
	}
}

// placeOf is a Dawarich site's key, else the point's grid cell. A site
// only Kimai knows counts as its cell: the background training reads
// Dawarich alone and must see the same place.
func placeOf(s *Site, lat, lon float64) string {
	if s != nil && (s.AreaID != 0 || s.PlaceID != 0) {
		return s.Key
	}
	return "c:" + strconv.Itoa(int(math.Floor(lat/cellDeg))) + "," + strconv.Itoa(int(math.Floor(lon/cellDeg)))
}
