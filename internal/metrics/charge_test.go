package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestChargeBusiness: each session's energy drives the car until the next
// session; its cost is shared by the km of business and other car rides
// in between. Bus rides and sessions outside the month do not count.
func TestChargeBusiness(t *testing.T) {
	at := func(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC) }
	ride := func(day int, km float64, mode string, class metrics.RideClass) metrics.ClassedRide {
		return metrics.ClassedRide{Ride: metrics.Ride{Start: at(day, 9), End: at(day, 10), KM: km, Mode: mode}, Class: class}
	}
	sessions := []sources.EVCCSession{ // newest first, as the source keeps them
		{Created: at(20, 18), Finished: at(20, 22), KWh: 10, Price: 3},
		{Created: at(10, 18), Finished: at(10, 22), KWh: 20, Price: 6},
		{Created: at(1, 18), Finished: at(1, 22), KWh: 20, Price: 9}, // the private ride of the 5th is its own
		{Created: time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC), Finished: time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC), Price: 50},
	}
	rides := []metrics.ClassedRide{
		ride(5, 100, metrics.ModeDriving, metrics.ClassPrivate),
		ride(12, 60, metrics.ModeDriving, metrics.ClassBusiness), // session of the 10th: 60 of 80 km
		ride(13, 20, metrics.ModeDriving, metrics.ClassPrivate),
		ride(14, 300, "bus", metrics.ClassBusiness),
		ride(22, 50, metrics.ModeDriving, metrics.ClassBusiness), // session of the 20th: all business
	}
	got := metrics.ChargeBusiness(sessions, rides, at(1, 0), at(30, 0))
	if got.Sessions != 3 || got.KWh != 50 || got.Cost != 18 || got.KM != 230 || got.BusinessKM != 110 {
		t.Fatalf("sums %+v", got)
	}
	if got.BusinessCost != 6*60.0/80+3 {
		t.Fatalf("business cost %+v", got)
	}
}
