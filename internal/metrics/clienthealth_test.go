package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// A customer's state: overdue money is a risk, a revenue drop with no
// work for months too, a drop alone is to watch, a rise is noted.
func TestClientHealth(t *testing.T) {
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		card  ClientCard
		state ClientState
		sig   string
	}{
		"overdue":  {ClientCard{Overdue: 100, LastWork: "2026-10-01"}, ClientRisk, "overdue"},
		"fading":   {ClientCard{Recent: 1000, Prior: 5000, LastWork: "2026-05-01"}, ClientRisk, "declining"},
		"dropping": {ClientCard{Recent: 1000, Prior: 5000, LastWork: "2026-10-01"}, ClientWatch, "declining"},
		"growing":  {ClientCard{Recent: 9000, Prior: 5000, LastWork: "2026-10-01"}, ClientOK, "growing"},
	} {
		h := healthOf(tc.card, today)
		if h.State != tc.state || len(h.Signals) == 0 || h.Signals[0] != tc.sig {
			t.Errorf("%s: %+v", name, h)
		}
	}
}

// The rate billed lately counts from where it began; after a year a
// raise by inflation is proposed.
func TestRateSince(t *testing.T) {
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 1, Name: "Werft"}}, Timesheets: []sources.KimaiSheet{
		{Begin: "2024-06-01T09:00:00", Minutes: 60, HourlyRate: 70, Billable: true, CustomerID: 1},
		{Begin: "2025-03-01T09:00:00", Minutes: 60, HourlyRate: 80, Billable: true, CustomerID: 1},
		{Begin: "2026-10-01T09:00:00", Minutes: 60, HourlyRate: 80, Billable: true, CustomerID: 1},
	}}
	c := ClientCards(kimai, nil, today, CenterMedian, nil, Span{})[0]
	if c.Rate != 80 || c.RateSince != "2025-03-01" || c.Raise < 82 || c.Raise > 83 || c.Health.Signals[len(c.Health.Signals)-1] != "raise" {
		t.Fatalf("card: rate %v since %s raise %v %+v", c.Rate, c.RateSince, c.Raise, c.Health)
	}
}

// A project over its budget comes first; one without work is left out.
func TestRecalc(t *testing.T) {
	kimai := &sources.KimaiDataset{Projects: []sources.KimaiProject{
		{Name: "Spot", CustomerID: 1, Budget: 2000, UsedMoney: 2400, UsedMinutes: 30 * 60},
		{Name: "Serie", CustomerID: 1, TimeBudgetMin: 100 * 60, UsedMoney: 4000, UsedMinutes: 50 * 60},
		{Name: "Leer", CustomerID: 1},
		{Name: "Fremd", CustomerID: 2, UsedMinutes: 60},
	}}
	got := Recalc(kimai, 1)
	if len(got) != 2 || got[0].Name != "Spot" || got[0].Share != 1.2 || got[0].PerHour != 80 || got[1].Share != 0.5 {
		t.Fatalf("recalc: %+v", got)
	}
}
