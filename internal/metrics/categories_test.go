package metrics_test

import (
	"testing"

	"andon/internal/bayes"
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

// A tag votes for every expense its debits paid, like a category.
func TestTagVotes(t *testing.T) {
	n := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{{Key: "e1", Date: "2026-09-02", Amount: 899, CategoryKey: "gear"}}}
	s := &sources.BankData{Transactions: []sources.BankTxn{
		{Date: "2026-09-03", Amount: -899, Tags: []string{"Studio", "Licht"}},
		{Date: "2026-09-03", Amount: -899}, // no tag
	}}

	votes := metrics.SureTagVotes(n, s)
	for _, tag := range []string{"Studio", "Licht"} {
		if best, count := votes.Best(tag); best != "gear" || count != 1 {
			t.Errorf("%s: %s %d (%v)", tag, best, count, votes)
		}
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
	cases := metrics.EuerCases(n, 3, "2026-09-01", bayes.Use{})
	if len(cases) != 3 || cases[2].Expense.Key != "open1" || cases[2].Line != "GWG" || cases[0].Line != "" || cases[1].Line != "" {
		t.Errorf("cases: %+v", cases)
	}
}

// A new vendor gets the line of expenses with the same words; its own
// last line wins once it has one; a line outside the dropdown is never
// proposed.
func TestEuerGuesser(t *testing.T) {
	n := &sources.NinjaDataset{
		Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Hetzner Online GmbH"}, {Key: "v2", Name: "teilAuto eG"}, {Key: "v3", Name: "Kagi Inc"}},
		Expenses: []sources.NinjaExpense{
			{Key: "e1", VendorKey: "v1", Date: "2026-01-05", Amount: 11.43, Notes: "Server", Custom: [4]string{"", "", "EDV"}},
			{Key: "e2", VendorKey: "v1", Date: "2026-02-05", Amount: 11.43, Notes: "Server Cloud", Custom: [4]string{"", "", "EDV"}},
			{Key: "e3", VendorKey: "v2", Date: "2026-02-07", Amount: 148.68, Notes: "Carsharing Fahrt", Custom: [4]string{"", "", "Reise"}},
			{Key: "e4", VendorKey: "v3", Date: "2026-02-08", Amount: 4.40, Notes: "Suche", Custom: [4]string{"", "", "EDV"}},
		},
	}
	g := metrics.EuerGuesserOf(n, 3, bayes.Use{Model: bayes.New(), MinP: metrics.EuerMinP})

	if line, from := g.Guess(metrics.EuerInput{Vendor: "Netcup GmbH", Texts: []string{"Cloud Server"}, Amount: 9}); line != "EDV" || from != metrics.EuerFromLearned {
		t.Errorf("new hoster: %q %q", line, from)
	}
	if line, from := g.Guess(metrics.EuerInput{Vendor: "Stadtmobil", Texts: []string{"Carsharing Fahrt"}, Amount: 120}); line != "Reise" || from != metrics.EuerFromLearned {
		t.Errorf("car sharing: %q %q", line, from)
	}
	if line, from := g.Guess(metrics.EuerInput{VendorKey: "v2", Vendor: "teilAuto eG", Texts: []string{"Server"}}); line != "Reise" || from != metrics.EuerFromVendor {
		t.Errorf("known vendor: %q %q", line, from)
	}
	// The amount alone is no evidence.
	for _, amount := range []float64{0, 4.80, 9.80} {
		if line, _ := g.Guess(metrics.EuerInput{Vendor: "Bäckerei Schmidt", Texts: []string{"Brötchen"}, Amount: amount}); line != "" {
			t.Errorf("nothing known at %v proposes %q", amount, line)
		}
	}

	n.ExpenseFields[2] = sources.ExpenseField{Label: "EÜR", Options: []string{"EDV"}}
	g = metrics.EuerGuesserOf(n, 3, bayes.Use{Model: bayes.New(), MinP: metrics.EuerMinP})
	if line, _ := g.Guess(metrics.EuerInput{Texts: []string{"Carsharing"}}); line != "" {
		t.Errorf("line outside the dropdown proposed: %q", line)
	}
}

// A line the user emptied is forgotten, not proposed back; an expense
// gone from Invoice Ninja stays learned.
func TestEuerLearnForgetsEmptied(t *testing.T) {
	n := &sources.NinjaDataset{
		Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Hetzner"}, {Key: "v2", Name: "Kagi"}},
		Expenses: []sources.NinjaExpense{
			{Key: "e1", VendorKey: "v1", Notes: "Server", Custom: [4]string{"", "", "EDV"}},
			{Key: "e2", VendorKey: "v2", Notes: "Suche", Custom: [4]string{"", "", "EDV"}},
		},
	}
	m := bayes.New()
	metrics.EuerLearn(m, n, 3)

	n.Expenses[0].Custom[2] = "" // emptied
	n.Expenses = n.Expenses[:1]  // e2 deleted
	if !metrics.EuerLearn(m, n, 3) || m.Stats().Examples != 1 {
		t.Fatalf("emptied line kept: %+v", m.Stats())
	}
}
