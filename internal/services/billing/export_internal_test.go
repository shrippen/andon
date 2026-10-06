package billing

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestTripRows: business rides and commutes of the year, business km
// priced at the km rate, private rides left out.
func TestTripRows(t *testing.T) {
	day := time.Date(2026, 3, 2, 9, 0, 0, 0, time.Local)
	acme := &metrics.Site{Name: "Acme", Kind: metrics.KindCustomer, CustomerID: 7}
	ride := func(class metrics.RideClass, km float64) metrics.ClassedRide {
		return metrics.ClassedRide{Ride: metrics.Ride{Start: day, End: day.Add(30 * time.Minute), KM: km, Mode: metrics.ModeDriving},
			To: acme, Class: class, Reason: metrics.ReasonCustomer, CustomerID: 7}
	}
	travel := metrics.Travel{Rides: []metrics.ClassedRide{ride(metrics.ClassBusiness, 20), ride(metrics.ClassPrivate, 5)}}
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 7, Name: "Acme GmbH"}}}

	rows := tripRows(travel, kimai, 2026, 0.30)

	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	r := rows[1]
	if r[0] != "2026-03-02" || r[1] != "09:00" || r[4] != "Acme" || r[5] != "beruflich" || r[6] != "Acme GmbH" || r[8] != "20,00" || r[9] != "6,00" || r[10] != "Kundenort" {
		t.Fatalf("row: %v", r)
	}
}
