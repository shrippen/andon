package metrics_test

import (
	"strings"
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
		BankRead:   true,
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

// The models only suggest: an unnamed Amazon booking gets its vendor
// and category as suggestions (every tax is 19 %: one class, nothing to
// suggest), the fields stay empty; the bakery on
// the mixed account suggests private, the hoster a new expense, and
// neither changes its action. Without the models nothing is suggested.
func TestBankLearning(t *testing.T) {
	n := learnedNinja()
	in := metrics.VATInputs{Ninja: n, Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Today: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}
	plain := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(in) {
		plain[c.Txn.Key] = c
	}
	in.Learned = learnedOf(n)
	byKey := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(in) {
		byKey[c.Txn.Key] = c
	}

	d := byKey["o1"].Draft
	if d.VendorKey != "" || d.CategoryKey != "" || d.TaxKnown {
		t.Errorf("Amazon filled: %+v", d)
	}
	if first(d.Suggest.Vendor) != "Versandhandel Europa S.à r.l." || first(d.Suggest.Category) != "c1" || d.Suggest.Tax != nil {
		t.Errorf("Amazon suggests: %+v", d.Suggest)
	}
	for key, want := range map[string]metrics.BankAction{"o2": metrics.ActionSkip, "o3": metrics.ActionCreate} {
		c := byKey[key]
		if c.Action != plain[key].Action || first(c.Suggest) != string(want) {
			t.Errorf("%s: action %s (without model %s), suggests %+v", key, c.Action, plain[key].Action, c.Suggest)
		}
	}

	in.Learned = metrics.BankLearning{}
	for _, c := range metrics.BankCases(in) {
		if c.Suggest != nil || !c.Draft.Suggest.None() {
			t.Errorf("%s suggests without models", c.Txn.Key)
		}
	}
}

// first is the value of a suggestion line's first pick, "" none.
func first(ps []metrics.Pick) string {
	if len(ps) == 0 {
		return ""
	}
	return ps[0].Value
}

// A new vendor without history gets the learned category and tax as
// suggestions; an expense without tax teaches "none".
func TestLearnedDraft(t *testing.T) {
	n := learnedNinja()
	n.Expenses[2].TaxName, n.Expenses[2].TaxRate = "", 0
	l := learnedOf(n)
	l.Vendor = bayes.Use{} // the hoster stays a new vendor

	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Hostingfirma Server", Description: "Server Miete", Amount: 9.9, Debit: true}, l)
	if d.CategoryKey != "" || d.TaxKnown || first(d.Suggest.Category) != "c2" || first(d.Suggest.Tax) != "none" || d.Suggest.Tax[0].Text != "vatcheck.no_tax" {
		t.Fatalf("draft: %+v", d)
	}
}

// What a fixed rule fills is not suggested: a vendor named in the
// booking brings its history, and the models stay silent on it.
func TestFilledNotSuggested(t *testing.T) {
	n := learnedNinja()
	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Nordhost GmbH", Description: "Server Miete", Amount: 9.9, Debit: true}, learnedOf(n))
	if d.VendorKey != "v2" || d.From != metrics.FromHistory || !d.Suggest.None() {
		t.Fatalf("draft: %+v", d)
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

// A failed bank fetch (no bookings, BankRead false) neither teaches nor
// forgets; an expense whose booking only left the window stays learned.
func TestLearnKeepsWithoutBank(t *testing.T) {
	n := learnedNinja()
	n.BankRead = true
	m := bayes.New()
	metrics.LearnVendors(m, n)
	before := m.Stats().Examples

	failed := *n
	failed.BankRead, failed.BankTxns = false, nil
	if metrics.LearnVendors(m, &failed) || m.Stats().Examples != before {
		t.Errorf("failed fetch: %d → %d", before, m.Stats().Examples)
	}
	window := *n
	window.BankTxns = n.BankTxns[1:] // t1 left the window, e1 still links it
	if metrics.LearnVendors(m, &window); m.Stats().Examples != before {
		t.Errorf("booking out of window: %d → %d", before, m.Stats().Examples)
	}
}

// A field filled after the draft (Sure's mapping, Claude's reading)
// loses its suggestion line; an open one keeps it. The vendor field
// always holds text (the payer): its line goes only with a known vendor.
func TestDropFilled(t *testing.T) {
	picks := []metrics.Pick{{Value: "x", Label: "x"}}
	d := metrics.ExpenseDraft{Vendor: "AMZN", CategoryKey: "c1", Suggest: metrics.DraftSuggest{Vendor: picks, Category: picks, Tax: picks, Euer: picks}}
	d.DropFilled()
	if d.Suggest.Vendor == nil || d.Suggest.Category != nil || d.Suggest.Tax == nil || d.Suggest.Euer == nil {
		t.Errorf("category filled: %+v", d.Suggest)
	}
	d.VendorKey, d.TaxKnown, d.Euer = "v1", true, "EDV"
	d.DropFilled()
	if !d.Suggest.None() {
		t.Errorf("all filled: %+v", d.Suggest)
	}
}

// A vendor's last expense was half its booking, reckoned in a private
// note: both are offered, neither filled.
func TestSuggestLastShare(t *testing.T) {
	n := &sources.NinjaDataset{BankRead: true, Vendors: []sources.NinjaVendor{{Key: "v", Name: "Fernverkehr"}},
		BankTxns: []sources.NinjaBankTxn{{Key: "t1", Amount: 63, Debit: true}},
		Expenses: []sources.NinjaExpense{{Key: "e", VendorKey: "v", Date: "2026-09-02", Amount: 31.5, TxnKey: "t1", PrivateNote: "Nutzung 50/50"}}}
	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Fernverkehr", Amount: 63, Debit: true}, metrics.BankLearning{})
	if len(d.Suggest.Share) != 1 || d.Suggest.Share[0].Value != "50" || len(d.Suggest.Private) != 1 || d.Suggest.Private[0].Value != "Nutzung 50/50" {
		t.Errorf("suggest: %+v", d.Suggest)
	}
}

// Only a real share is offered: a net amount with its tax beside it is
// the whole booking, and so are the parts of a split.
func TestSuggestLastNoShare(t *testing.T) {
	vendors := []sources.NinjaVendor{{Key: "v", Name: "Fernverkehr"}}
	txns := []sources.NinjaBankTxn{{Key: "t1", Amount: 100, Debit: true}}
	cases := map[string][]sources.NinjaExpense{
		"net plus tax": {{Key: "e", VendorKey: "v", Date: "2026-09-02", Amount: 84.03, Tax: 15.97, TaxRate: 19, TxnKey: "t1"}},
		"split": {{Key: "e", VendorKey: "v", Date: "2026-09-02", Amount: 60, Gross: true, TxnKey: "t1"},
			{Key: "f", VendorKey: "w", Date: "2026-09-01", Amount: 40, Gross: true, TxnKey: "t1"}},
	}
	for name, expenses := range cases {
		n := &sources.NinjaDataset{BankRead: true, Vendors: vendors, BankTxns: txns, Expenses: expenses}
		d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Fernverkehr", Amount: 100, Debit: true}, metrics.BankLearning{})
		if len(d.Suggest.Share) != 0 {
			t.Errorf("%s: suggest %+v", name, d.Suggest.Share)
		}
	}
}

// shareNinja: the phone bill is booked at half with its reckoning, the
// hoster whole without a note; a third vendor's last expense went half
// although nothing of its booking is known to the models.
func shareNinja() *sources.NinjaDataset {
	txn := func(key, payer string, amount float64) sources.NinjaBankTxn {
		return sources.NinjaBankTxn{Key: key, Date: "2026-09-02", Payer: payer, Amount: amount, Debit: true}
	}
	exp := func(key, vendor, txn string, amount float64, note string) sources.NinjaExpense {
		return sources.NinjaExpense{Key: key, VendorKey: vendor, Date: "2026-09-02", TxnKey: txn, Amount: amount, Gross: true, PrivateNote: note}
	}
	return &sources.NinjaDataset{BankRead: true,
		Vendors:  []sources.NinjaVendor{{Key: "m", Name: "Funknetz"}, {Key: "h", Name: "Serverhof"}, {Key: "z", Name: "Zeitung"}},
		BankTxns: []sources.NinjaBankTxn{txn("t1", "Funknetz Mobilfunk", 60), txn("t2", "Funknetz Mobilfunk", 62), txn("t3", "Serverhof Miete", 10), txn("t4", "Serverhof Miete", 12), txn("t5", "Altkunde", 40)},
		Expenses: []sources.NinjaExpense{exp("e1", "m", "t1", 30, "Nutzung 50/50"), exp("e2", "m", "t2", 31, "Nutzung 50/50"), exp("e3", "h", "t3", 10, ""), exp("e4", "h", "t4", 12, ""), exp("e5", "z", "t5", 20, "Altes Abo")}}
}

func shareLearning(n *sources.NinjaDataset) metrics.BankLearning {
	share, note := bayes.New(), bayes.New()
	metrics.LearnShares(share, n)
	metrics.LearnNotes(note, n)
	return metrics.BankLearning{Share: bayes.Use{Model: share, MinP: 0.6}, Note: bayes.Use{Model: note, MinP: 0.6}}
}

// The models suggest share and note by the booking's words, of a vendor
// not known too; "whole" and "no note" are never offered.
func TestLearnedShareNote(t *testing.T) {
	n := shareNinja()
	l := shareLearning(n)

	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Mobilfunk Rechnung", Amount: 70, Debit: true}, l)
	if len(d.Suggest.Share) == 0 || d.Suggest.Share[0].Value != "50" || d.Suggest.Share[0].Label != "50 %" {
		t.Errorf("share: %+v", d.Suggest.Share)
	}
	if len(d.Suggest.Private) == 0 || d.Suggest.Private[0].Value != "Nutzung 50/50" {
		t.Errorf("note: %+v", d.Suggest.Private)
	}

	d = metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Serverhof Miete", Amount: 11, Debit: true}, l)
	if d.Suggest.Share != nil || d.Suggest.Private != nil {
		t.Errorf("a whole booking got %+v and %+v", d.Suggest.Share, d.Suggest.Private)
	}
}

// While the models know nothing of the booking, the vendor's last expense
// speaks; when they have answered "whole", it does not.
func TestLastStandsIn(t *testing.T) {
	n := shareNinja()
	l := shareLearning(n)

	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Zeitung", Amount: 40, Debit: true}, l)
	if len(d.Suggest.Share) != 1 || d.Suggest.Share[0].Value != "50" || len(d.Suggest.Private) != 1 || d.Suggest.Private[0].Value != "Altes Abo" {
		t.Errorf("standing in: %+v", d.Suggest)
	}

	d = metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Serverhof Miete", Amount: 11, Debit: true}, l)
	if d.Suggest.Share != nil {
		t.Errorf("model said whole: %+v", d.Suggest.Share)
	}
}

// A note no expense holds any more is not offered: the model keeps its
// hash only. No note text reaches the stored model.
func TestNoteGoneNotOffered(t *testing.T) {
	n := shareNinja()
	l := shareLearning(n)
	raw, err := l.Note.Model.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Nutzung") {
		t.Fatalf("model stores the note: %s", raw)
	}

	for i := range n.Expenses {
		n.Expenses[i].PrivateNote = ""
	}
	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Mobilfunk Rechnung", Amount: 70, Debit: true}, l)
	if d.Suggest.Private != nil {
		t.Errorf("gone note offered: %+v", d.Suggest.Private)
	}
}

// A split's part teaches no share; an unlinked expense is forgotten.
func TestLearnSharesParts(t *testing.T) {
	n := shareNinja()
	m := bayes.New()
	metrics.LearnShares(m, n)
	before := m.Stats().Examples

	n.Expenses = append(n.Expenses, sources.NinjaExpense{Key: "e6", TxnKey: "t1", Amount: 29, Gross: true})
	metrics.LearnShares(m, n)
	if got := m.Stats().Examples; got != before-1 {
		t.Errorf("a split's parts stayed: %d examples, had %d", got, before)
	}
}

// An expense lacking its category gets the model's by its booking, else
// the vendor's last one.
func TestSuggestCategory(t *testing.T) {
	n := learnedNinja()
	m := bayes.New()
	metrics.LearnCategories(m, n)
	l := metrics.BankLearning{Category: bayes.Use{Model: m, MinP: 0.6}}

	picks := metrics.SuggestCategory(n, sources.NinjaExpense{Key: "x", TxnKey: "o1"}, l)
	if len(picks) == 0 || picks[0].Value != "c1" {
		t.Errorf("by the booking: %+v", picks)
	}
	picks = metrics.SuggestCategory(n, sources.NinjaExpense{Key: "x", VendorKey: "v2"}, metrics.BankLearning{})
	if len(picks) != 1 || picks[0].Value != "c2" {
		t.Errorf("by the vendor's last: %+v", picks)
	}
	if picks := metrics.SuggestCategory(n, sources.NinjaExpense{Key: "x"}, metrics.BankLearning{}); picks != nil {
		t.Errorf("from nothing: %+v", picks)
	}
}

// A split's blank part takes category and tax suggestions from the booking,
// also where its vendor is known and would fill them on a whole expense.
func TestSuggestPart(t *testing.T) {
	n := learnedNinja()
	cat := bayes.New()
	metrics.LearnCategories(cat, n)
	s := metrics.SuggestPart(n, sources.NinjaBankTxn{Payer: "AMZN Mktp DE*QQ11WW", Description: "Amazon Marketplace", Amount: 24.99, Debit: true},
		metrics.BankLearning{Category: bayes.Use{Model: cat, MinP: 0.6}})
	if len(s.Category) == 0 || s.Category[0].Value != "c1" {
		t.Errorf("part: %+v", s)
	}
}

// A share stays what was booked. Only a whole-euro rounding of a round
// share counts as it: 32 € of 63 € is half, 21 € of 100 € is not a fifth.
func TestShareStaysBooked(t *testing.T) {
	for _, c := range []struct {
		booked, of float64
		want       string
	}{
		{20, 60, "33"}, {40, 60, "67"}, {7.2, 60, "12"}, {58.2, 60, "97"}, {30.6, 60, "51"},
		{21, 100, "21"}, {49, 100, "49"}, {210, 1000, "21"}, {1, 100, "1"}, {99, 100, "99"},
		{32, 63, "50"}, {30.4, 60, "50"}, {26, 51.99, "50"},
		{4, 12, "33"}, {5, 15, "33"}, {2, 8, "25"}, {2.5, 10, "25"}, {9.5, 10, "95"},
	} {
		txn := func(key, payer string, amount float64) sources.NinjaBankTxn {
			return sources.NinjaBankTxn{Key: key, Date: "2026-09-02", Payer: payer, Amount: amount, Debit: true}
		}
		exp := func(key, txn string, amount float64) sources.NinjaExpense {
			return sources.NinjaExpense{Key: key, Date: "2026-09-02", TxnKey: txn, Amount: amount, Gross: true}
		}
		n := &sources.NinjaDataset{BankRead: true,
			BankTxns: []sources.NinjaBankTxn{txn("t1", "Funknetz Mobilfunk", c.of), txn("t2", "Funknetz Mobilfunk", c.of), txn("t3", "Serverhof", 10), txn("t4", "Serverhof", 10)},
			Expenses: []sources.NinjaExpense{exp("e1", "t1", c.booked), exp("e2", "t2", c.booked), exp("e3", "t3", 10), exp("e4", "t4", 10)}}
		d := metrics.DraftWith(n, txn("o", "Funknetz Mobilfunk", c.of), shareLearning(n))
		if len(d.Suggest.Share) != 1 || d.Suggest.Share[0].Value != c.want {
			t.Errorf("booked %v of %v: %+v, want %s", c.booked, c.of, d.Suggest.Share, c.want)
		}
	}
}

// The expense filled is often its vendor's newest: the vendor's last
// category is the newest one that has one.
func TestSuggestCategorySelf(t *testing.T) {
	n := &sources.NinjaDataset{Categories: []sources.NinjaCategory{{Key: "c", Name: "Telefon"}},
		Expenses: []sources.NinjaExpense{{Key: "e1", VendorKey: "v", Date: "2026-08-01", CategoryKey: "c"}, {Key: "e2", VendorKey: "v", Date: "2026-09-01"}}}
	picks := metrics.SuggestCategory(n, n.Expenses[1], metrics.BankLearning{})
	if len(picks) != 1 || picks[0].Value != "c" {
		t.Errorf("picks: %+v", picks)
	}
}

// An expense without a date still has a category to offer.
func TestSuggestCategoryNoDate(t *testing.T) {
	n := &sources.NinjaDataset{Categories: []sources.NinjaCategory{{Key: "c", Name: "Telefon"}},
		Expenses: []sources.NinjaExpense{{Key: "e1", VendorKey: "v", CategoryKey: "c"}, {Key: "e2", VendorKey: "v", Date: "2026-09-01"}}}
	if picks := metrics.SuggestCategory(n, n.Expenses[1], metrics.BankLearning{}); len(picks) != 1 {
		t.Errorf("picks: %+v", picks)
	}
}

// The vendor's last expense counts like the model: rounded euros are the round share.
func TestLastShareRounded(t *testing.T) {
	n := &sources.NinjaDataset{BankRead: true, Vendors: []sources.NinjaVendor{{Key: "v", Name: "Fernverkehr"}},
		BankTxns: []sources.NinjaBankTxn{{Key: "t1", Amount: 63, Debit: true}},
		Expenses: []sources.NinjaExpense{{Key: "e", VendorKey: "v", Date: "2026-09-02", Amount: 32, Gross: true, TxnKey: "t1"}}}
	d := metrics.DraftWith(n, sources.NinjaBankTxn{Payer: "Fernverkehr", Amount: 63, Debit: true}, metrics.BankLearning{})
	if len(d.Suggest.Share) != 1 || d.Suggest.Share[0].Value != "50" {
		t.Errorf("share: %+v", d.Suggest.Share)
	}
}
