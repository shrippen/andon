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
	euer := metrics.EuerVotes(n, 3)
	if best, _ := euer.Best("lic"); best != "Laufende EDV-Kosten" {
		t.Errorf("EÜR: %v", euer)
	}

	cases := metrics.EuerCases(n, 3, map[string]string{"lic": "Laufende EDV-Kosten"}, "2026-09-01")
	if len(cases) != 2 || cases[0].Expense.Key != "e3" || cases[0].Line != "" || cases[1].Line != "Laufende EDV-Kosten" {
		t.Errorf("cases: %+v", cases)
	}
}
