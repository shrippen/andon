package metrics_test

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func TestCategoryVotes(t *testing.T) {
	n := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{
		{Key: "e1", Date: "2026-09-02", Amount: 12.5, CategoryKey: "lic"},
		{Key: "e2", Date: "2026-09-20", Amount: 30, CategoryKey: "lic"},
		{Key: "e3", Date: "2026-09-21", Amount: 30, CategoryKey: "travel"},
	}}
	n.Expenses[0].Custom[2] = "Laufende EDV-Kosten"
	s := &sources.BankData{Transactions: []sources.BankTxn{
		{Date: "2026-09-03", Amount: -12.5, Category: "Software"},
		{Date: "2026-09-19", Amount: -30, Category: "Software"},
		{Date: "2026-10-30", Amount: -30, Category: "Software"},  // no expense near
		{Date: "2026-09-02", Amount: 12.5, Category: "Software"}, // income
	}}

	votes := metrics.SureCategoryVotes(n, s)
	if best, count := votes.Best("Software"); best != "lic" || count != 2 {
		t.Errorf("Software: %s %d (%v)", best, count, votes)
	}
}

// The EÜR line comes from the vendor's latest expense with one, never
// from the category: the same category goes to different lines.
func TestEuerFromVendor(t *testing.T) {
	n := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{
		{Key: "old", Date: "2026-08-01", VendorKey: "v1", CategoryKey: "lic"},
		{Key: "new", Date: "2026-08-15", VendorKey: "v1", CategoryKey: "lic"},
		{Key: "open1", Date: "2026-09-02", VendorKey: "v1", CategoryKey: "lic"},
		{Key: "open2", Date: "2026-09-20", VendorKey: "v2", CategoryKey: "lic"},
		{Key: "open3", Date: "2026-09-21", CategoryKey: "lic"},
	}}
	n.Expenses[0].Custom[2] = "Laufende EDV-Kosten"
	n.Expenses[1].Custom[2] = "GWG"

	if got := metrics.VendorEuer(n, 3); len(got) != 1 || got["v1"] != "GWG" {
		t.Errorf("vendor lines: %v", got)
	}
	cases := metrics.EuerCases(n, 3, "2026-09-01")
	if len(cases) != 3 || cases[2].Expense.Key != "open1" || cases[2].Line != "GWG" || cases[0].Line != "" || cases[1].Line != "" {
		t.Errorf("cases: %+v", cases)
	}
}
