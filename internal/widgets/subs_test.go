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
