package widgets_test

import (
	"testing"

	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestSubscriptionsTile: with Wallos, its active subscriptions (sum per
// month, next debits first) and what Sure pays that Wallos lacks;
// without Wallos, Sure's recurring payments.
func TestSubscriptionsTile(t *testing.T) {
	kind, ok := widgets.Get("subscriptions")
	if !ok {
		t.Fatal("subscriptions not registered")
	}
	cfg, _ := widgets.Decode("subscriptions", map[string]any{})
	wallos := &sources.WallosDataset{Currency: "EUR", Subs: []sources.WallosSub{
		{Name: "Tibber", Monthly: 72, Price: 72, Next: "2026-10-03"},
		{Name: "Hetzner", Monthly: 38.2, Price: 38.2, Next: "2026-09-20"},
		{Name: "Alt", Monthly: 5, Inactive: true, Next: "2026-09-16"},
	}}
	sure := &sources.SureDataset{Currency: "EUR", Recurring: []sources.SureRecurring{
		{Name: "Adobe Creative Cloud", Status: "active", Amount: 66.45, Expense: true, Next: "2026-10-05"}}}

	view := kind.View(cfg, map[string]any{"wallos": wallos, "sure": sure}, ctxFor("", nil))
	rows := view["Rows"].([]widgets.SubRow)
	if view["Monthly"] != 110.2 || len(rows) != 2 || rows[0].Name != "Hetzner" || view["Source"] != "wallos" {
		t.Fatalf("wallos view: %+v", view)
	}
	if view["Missing"] != "Adobe Creative Cloud" {
		t.Fatalf("missing: %+v", view["Missing"])
	}

	view = kind.View(cfg, map[string]any{"sure": sure}, ctxFor("", nil))
	if view["Source"] != "sure" || len(view["Rows"].([]widgets.SubRow)) != 1 {
		t.Fatalf("sure view: %+v", view)
	}
}

// TestSubscriptionsSureCategory: Sure's recurring payments carry no
// category; the latest booking of the same name lends its own, so the
// category filter keeps them.
func TestSubscriptionsSureCategory(t *testing.T) {
	kind, _ := widgets.Get("subscriptions")
	cfg, _ := widgets.Decode("subscriptions", map[string]any{"categories": []any{"Software"}})
	sure := &sources.SureDataset{Currency: "EUR",
		Recurring: []sources.SureRecurring{
			{Name: "Adobe", Status: "active", Amount: 66, Expense: true, Next: "2026-10-05"},
			{Name: "Tibber", Status: "active", Amount: 72, Expense: true, Next: "2026-10-03"}},
		Transactions: []sources.SureTxn{
			{Date: "2026-09-05", Name: "Adobe", Amount: -66, Category: "Software"},
			{Date: "2026-09-03", Merchant: "Tibber", Amount: -72, Category: "Energie"}}}

	rows := kind.View(cfg, map[string]any{"sure": sure}, ctxFor("", nil))["Rows"].([]widgets.SubRow)
	if len(rows) != 1 || rows[0].Name != "Adobe" || rows[0].Category != "Software" {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestSubscriptionsMonthlyRows: a yearly subscription lists at its
// monthly amount next to the monthly ones, as the sum counts it; per
// year all rows show the year.
func TestSubscriptionsMonthlyRows(t *testing.T) {
	kind, _ := widgets.Get("subscriptions")
	wallos := &sources.WallosDataset{Currency: "EUR", Subs: []sources.WallosSub{
		{Name: "Domain", Monthly: 2, Price: 24, Next: "2026-10-28"},
		{Name: "Tibber", Monthly: 72, Price: 72, Next: "2026-10-03"},
	}}
	for _, c := range []struct {
		raw  map[string]any
		want float64
	}{{map[string]any{}, 2}, {map[string]any{"yearly": true}, 24}} {
		cfg, _ := widgets.Decode("subscriptions", c.raw)
		for _, r := range kind.View(cfg, map[string]any{"wallos": wallos}, ctxFor("", nil))["Rows"].([]widgets.SubRow) {
			if r.Name == "Domain" && r.Price != c.want {
				t.Fatalf("%v: domain at %v, want %v", c.raw, r.Price, c.want)
			}
		}
	}
}
