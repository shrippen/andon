package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// A recent asset lacks what Snipe-IT and Invoice Ninja do not show: the
// laptop its warranty and receipt (its expense matches net within days),
// the cable its cost; an old one is not checked, a complete one passes.
func TestPurchases(t *testing.T) {
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	snipe := &sources.SnipeDataset{Assets: []sources.SnipeAsset{
		{ID: 1, Name: "Laptop", PurchaseDate: "2026-09-01", PurchaseCost: 1000},
		{ID: 2, Name: "Kabel", PurchaseDate: "2026-08-01", WarrantyExpires: "2028-08-01"},
		{ID: 3, Name: "Alt", PurchaseDate: "2024-01-01", PurchaseCost: 50},
		{ID: 4, Name: "Monitor", PurchaseDate: "2026-07-01", PurchaseCost: 300, WarrantyExpires: "2028-07-01"},
	}}
	ninja := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{
		{Number: "A-1", Date: "2026-09-04", Amount: 1190, Tax: 190},
		{Number: "A-2", Date: "2026-07-02", Amount: 300, Custom: [sources.NinjaSlots]string{"", "doc 7"}},
	}}
	got := Purchases(snipe.Inventory(), ninja, 2, today)
	if len(got) != 2 || got[0].Asset.Name != "Laptop" || got[1].Asset.Name != "Kabel" {
		t.Fatalf("cases: %+v", got)
	}
	if l := got[0]; !l.Has(GapWarranty) || !l.Has(GapReceipt) || l.Has(GapExpense) || l.Expense.Number != "A-1" {
		t.Errorf("laptop: %+v", l)
	}
	if k := got[1]; len(k.Gaps) != 1 || !k.Has(GapCost) {
		t.Errorf("cable: %+v", k)
	}
}
