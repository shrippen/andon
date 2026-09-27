package rules_test

import (
	"testing"

	"andon/internal/sources"
)

// TestWallosMissing: one hint names Sure's subscriptions Wallos lacks;
// none when Wallos has them all.
func TestWallosMissing(t *testing.T) {
	sure := &sources.SureDataset{Currency: "EUR", Recurring: []sources.SureRecurring{
		{Name: "Adobe Creative Cloud", Status: "active", Amount: 66.45, Expense: true},
		{Name: "Hetzner Online", Status: "active", Amount: 41.65, Expense: true},
	}}
	wallos := &sources.WallosDataset{URL: "https://wallos.test", Subs: []sources.WallosSub{{Name: "Hetzner"}}}
	got := run(t, "cross.wallos_missing", nil, crossEnv("2026-09-25", map[string]any{"sure": sure, "wallos": wallos}))
	if len(got) != 1 || got[0].Params["count"] != 1 || got[0].Params["names"] != "Adobe Creative Cloud" || got[0].ActionURL != "https://wallos.test" {
		t.Fatalf("findings: %+v", got)
	}
	wallos.Subs = append(wallos.Subs, sources.WallosSub{Name: "Adobe"})
	if got := run(t, "cross.wallos_missing", nil, crossEnv("2026-09-25", map[string]any{"sure": sure, "wallos": wallos})); len(got) != 0 {
		t.Fatalf("complete Wallos still hinted: %+v", got)
	}
}
