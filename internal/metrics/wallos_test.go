package metrics_test

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestNotInWallos: Sure's recurring spending that looks like a
// subscription (small, active, not ignored) but has no Wallos entry.
func TestNotInWallos(t *testing.T) {
	sure := &sources.SureDataset{Recurring: []sources.SureRecurring{
		{Name: "Hetzner Online", Status: "active", Amount: 41.65, Expense: true},
		{Name: "Adobe Creative Cloud", Status: "active", Amount: 66.45, Expense: true},
		{Name: "Miete Büro", Status: "active", Amount: 450, Expense: true},
		{Name: "Netflix", Status: "inactive", Amount: 13, Expense: true},
		{Name: "GEZ Rundfunk", Status: "active", Amount: 18.36, Expense: true},
		{Name: "Gehalt", Status: "active", Amount: 3000},
	}}
	wallos := &sources.WallosDataset{Subs: []sources.WallosSub{{Name: "Hetzner"}, {Name: "Adobe", Inactive: true}}}
	got := metrics.NotInWallos(sure, wallos, 100, []string{"gez"})
	if len(got) != 1 || got[0].Name != "Adobe Creative Cloud" {
		t.Fatalf("missing: %+v", got)
	}
}
