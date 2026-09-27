package rules_test

import (
	"testing"

	"andon/internal/rules"
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

// TestWallosRenewalSoon: a yearly subscription renewing within 30 days is
// the last chance to cancel; monthly ones renew all the time and stay
// quiet, as do inactive ones.
func TestWallosRenewalSoon(t *testing.T) {
	wallos := &sources.WallosDataset{URL: "https://wallos.test", Currency: "EUR", Subs: []sources.WallosSub{
		{Name: "Domain", Price: 24, Monthly: 2, Next: "2026-10-10"},
		{Name: "Hosting", Price: 9, Monthly: 9, Next: "2026-10-01"},
		{Name: "Old tool", Price: 120, Monthly: 10, Next: "2026-10-05", Inactive: true},
		{Name: "Later", Price: 60, Monthly: 5, Next: "2027-01-10"},
	}}
	got := run(t, "wallos.renewal_soon", wallos, rules.Env{Today: day("2026-09-25")})
	if len(got) != 1 || got[0].Fingerprint != "renew:Domain:2026-10-10" || got[0].Due != "2026-10-10" {
		t.Fatalf("findings: %+v", got)
	}
}
