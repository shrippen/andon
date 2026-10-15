package metrics

import (
	"testing"
	"time"
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
