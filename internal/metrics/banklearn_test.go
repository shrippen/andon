package metrics_test

import (
	"testing"
	"time"

	"andon/internal/bayes"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// learnedNinja: Amazon bought twice from the business account, booked
// with category Technik at 19 %; on the mixed account a bakery archived
// as private and a hoster booked; three open debits to sort.
func learnedNinja() *sources.NinjaDataset {
	txn := func(key, day, payer, text string, amount float64, role sources.AccountRole) sources.NinjaBankTxn {
		return sources.NinjaBankTxn{Key: key, Date: day, Payer: payer, Description: text, Amount: amount, Debit: true, Role: role, Status: sources.BankOpen}
	}
	booked := func(t sources.NinjaBankTxn, exp string) sources.NinjaBankTxn {
		t.Status, t.ExpenseKeys = sources.BankDone, []string{exp}
		return t
	}
	archived := func(t sources.NinjaBankTxn) sources.NinjaBankTxn {
		t.Archived = true
		return t
	}
	return &sources.NinjaDataset{
		Vendors:    []sources.NinjaVendor{{Key: "v1", Name: "Versandhandel Europa S.à r.l."}, {Key: "v2", Name: "Nordhost GmbH"}},
		Categories: []sources.NinjaCategory{{Key: "c1", Name: "Technik"}, {Key: "c2", Name: "Hosting"}},
		TaxRates:   []sources.NinjaTaxRate{{Name: "MwSt.", Rate: 19}, {Name: "MwSt. 7", Rate: 7}},
		BankTxns: []sources.NinjaBankTxn{
			booked(txn("t1", "2026-09-02", "AMZN Mktp DE*AB12CD", "Amazon Marketplace", 39.99, sources.RoleBusiness), "e1"),
			booked(txn("t2", "2026-09-20", "AMZN Mktp DE*ZZ99YY", "Amazon Marketplace", 12.49, sources.RoleBusiness), "e2"),
			archived(txn("t3", "2026-09-05", "Bäckerei Krume", "Brötchen Kartenzahlung", 4.80, sources.RoleMixed)),
			archived(txn("t4", "2026-09-12", "Bäckerei Krume", "Brötchen Kartenzahlung", 6.20, sources.RoleMixed)),
			booked(txn("t5", "2026-09-15", "Hostingfirma Server", "Server Miete", 9.90, sources.RoleMixed), "e3"),

			txn("o1", "2026-10-04", "AMZN Mktp DE*QQ11WW", "Amazon Marketplace", 24.99, sources.RoleBusiness),
			txn("o2", "2026-10-06", "Bäckerei Krume", "Brötchen Kartenzahlung", 5.10, sources.RoleMixed),
			txn("o3", "2026-10-07", "Hostingfirma Server", "Server Miete", 9.90, sources.RoleMixed),
		},
		Expenses: []sources.NinjaExpense{
			{Key: "e1", VendorKey: "v1", CategoryKey: "c1", TaxName: "MwSt.", TaxRate: 19, TxnKey: "t1", Date: "2026-09-02", Amount: 39.99},
			{Key: "e2", VendorKey: "v1", CategoryKey: "c1", TaxName: "MwSt.", TaxRate: 19, TxnKey: "t2", Date: "2026-09-20", Amount: 12.49},
			{Key: "e3", VendorKey: "v2", CategoryKey: "c2", TaxName: "MwSt.", TaxRate: 19, TxnKey: "t5", Date: "2026-09-15", Amount: 9.90},
		},
	}
}

func learnedOf(n *sources.NinjaDataset) metrics.BankLearning {
	use := func(teach func(*bayes.Model, *sources.NinjaDataset) bool) bayes.Use {
		m := bayes.New()
		teach(m, n)
		return bayes.Use{Model: m, MinP: metrics.EuerMinP}
	}
	return metrics.BankLearning{Vendor: use(metrics.LearnVendors), Private: use(metrics.LearnPrivate),
		Category: use(metrics.LearnCategories), Tax: use(metrics.LearnTaxes)}
}

// An unnamed Amazon booking finds its vendor (and so its category and
// tax); the bakery on the mixed account is proposed private, the hoster
// a new expense; without the models nothing of it.
func TestBankLearning(t *testing.T) {
	n := learnedNinja()
	in := metrics.VATInputs{Ninja: n, Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Today: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), Learned: learnedOf(n)}
	byKey := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(in) {
		byKey[c.Txn.Key] = c
	}

	if d := byKey["o1"].Draft; d.VendorKey != "v1" || d.VendorFrom != metrics.FromLearned || d.CategoryKey != "c1" || d.TaxRate != 19 {
		t.Errorf("Amazon: %+v", d)
	}
	if c := byKey["o2"]; c.Action != metrics.ActionSkip || !c.Learned {
		t.Errorf("bakery: %s learned %v", c.Action, c.Learned)
	}
	if c := byKey["o3"]; c.Action != metrics.ActionCreate || !c.Learned {
		t.Errorf("hoster: %s learned %v", c.Action, c.Learned)
	}

	in.Learned = metrics.BankLearning{}
	for _, c := range metrics.BankCases(in) {
		if c.Learned || c.Draft.VendorFrom == metrics.FromLearned {
			t.Errorf("%s learned without models", c.Txn.Key)
		}
	}
}

// A new vendor without history gets the learned category and tax; an
// expense without tax teaches "none".
func TestLearnedDraft(t *testing.T) {
	n := learnedNinja()
	n.Expenses[2].TaxName, n.Expenses[2].TaxRate = "", 0
	l := learnedOf(n)
	l.Vendor = bayes.Use{} // the hoster stays a new vendor

	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Hostingfirma Server", Description: "Server Miete", Amount: 9.9, Debit: true}, l)
	if d.CategoryKey != "c2" || d.From != metrics.FromLearned || !d.NoTax || d.TaxFrom != metrics.FromLearned {
		t.Fatalf("draft: %+v", d)
	}
}

// A learned vendor is no sure vendor: a mixed account's debit stays
// unsorted (not a ticked expense), and a transfer stays a transfer.
func TestLearnedVendorNotSure(t *testing.T) {
	n := learnedNinja()
	n.BankTxns = append(n.BankTxns,
		sources.NinjaBankTxn{Key: "m1", Date: "2026-10-09", Payer: "AMZN Mktp DE*PP00PP", Description: "Amazon Marketplace", Amount: 17.99,
			Debit: true, Role: sources.RoleMixed, Status: sources.BankOpen},
		sources.NinjaBankTxn{Key: "u1", Date: "2026-10-10", Payer: "Arian", Description: "Amazon Marketplace Umbuchung", Amount: 100,
			Debit: true, Role: sources.RoleBusiness, Status: sources.BankOpen},
		sources.NinjaBankTxn{Key: "u2", Date: "2026-10-10", Payer: "Arian", Description: "Umbuchung", Amount: 100,
			Role: sources.RolePrivate, Status: sources.BankOpen},
	)
	l := learnedOf(n)
	l.Private = bayes.Use{} // only the vendor model speaks
	in := metrics.VATInputs{Ninja: n, Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Today: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), Learned: l}
	byKey := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(in) {
		byKey[c.Txn.Key] = c
	}
	if c := byKey["m1"]; c.Action == metrics.ActionCreate || c.Draft.From == metrics.FromHistory {
		t.Errorf("mixed debit: %s from %s", c.Action, c.Draft.From)
	}
	if c := byKey["u1"]; c.Transfer == nil || c.Draft.From == metrics.FromHistory {
		t.Errorf("transfer: %s from %s transfer %v", c.Action, c.Draft.From, c.Transfer)
	}
}

// A decision taken back is forgotten: a booking brought back from
// private, an expense unlinked from its booking.
func TestLearnForgetsTakenBack(t *testing.T) {
	n := learnedNinja()
	m := bayes.New()
	metrics.LearnPrivate(m, n)
	before := m.Stats().Examples
	n.BankTxns[2].Archived = false // t3 restored
	if !metrics.LearnPrivate(m, n) || m.Stats().Examples != before-1 {
		t.Errorf("restored booking kept: %d → %d", before, m.Stats().Examples)
	}

	v := bayes.New()
	metrics.LearnVendors(v, n)
	before = v.Stats().Examples
	n.Expenses[0].TxnKey = "" // e1 unlinked
	if !metrics.LearnVendors(v, n) || v.Stats().Examples != before-1 {
		t.Errorf("unlinked expense kept: %d → %d", before, v.Stats().Examples)
	}
}
