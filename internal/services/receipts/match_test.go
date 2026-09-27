package receipts

import (
	"testing"

	"andon/internal/sources"
)

// PaperNinja's scorer cases, ported.

func expense(mod func(*sources.ReceiptExpense)) sources.ReceiptExpense {
	e := sources.ReceiptExpense{Key: "abc", Number: "EX-001", Amount: 42.5, Day: "2026-08-01", Vendor: "Acme GmbH"}
	if mod != nil {
		mod(&e)
	}
	return e
}

func doc(mod func(*sources.ReceiptDoc)) sources.ReceiptDoc {
	d := sources.ReceiptDoc{ID: 10, Title: "Rechnung Acme", Day: "2026-08-02", Added: "2026-08-03", Correspondent: "Acme GmbH",
		Content: "Gesamtbetrag 42,50 EUR Rechnungsnr RE-99", Custom: map[int64]any{}}
	if mod != nil {
		mod(&d)
	}
	return d
}

var mapped = Mapping{InvoiceSlot: 1, LinkSlot: 2, FieldInvoice: 1, FieldExpense: 2, FieldLink: 3}

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]float64{"42,50": 42.5, "1.234,56": 1234.56, "EUR 42.50": 42.5, "1,234.56": 1234.56, "EUR42.50": 42.5} {
		if got, ok := parseAmountText(in); !ok || got != want {
			t.Errorf("%q → %v, want %v", in, got, want)
		}
	}
	if got := amountsIn("Zwischensumme 10,00 Gesamt 1.042,50 Kunde 142,501"); len(got) != 2 || got[1] != 1042.5 {
		t.Fatalf("amounts: %v", got)
	}
}

func TestScoreStrongMatch(t *testing.T) {
	x := matcher{mapping: mapped}
	c := x.score(expense(func(e *sources.ReceiptExpense) { e.Custom[0] = "RE-99" }), doc(func(d *sources.ReceiptDoc) { d.Custom[1] = "RE-99" }))
	if c.Score < 70 || len(c.Factors) != 4 {
		t.Fatalf("score %d: %+v", c.Score, c.Factors)
	}
	for _, f := range c.Factors {
		if !f.Hit || f.Text == "" {
			t.Errorf("factor %s: %+v", f.Key, f)
		}
	}
	if where := c.Factors[3].Args["where"].(map[string]any); where["$t"] != "receipts.where_field" {
		t.Fatalf("invoice found in %v", c.Factors[3].Args["where"])
	}
}

// TestScoreDatePoints: closer is more, twice the window still counts a bit.
func TestScoreDatePoints(t *testing.T) {
	for apart, want := range map[int]int{0: 25, 7: 10, 8: 5, 14: 5, 15: 0} {
		if got := datePoints(apart); got != want {
			t.Errorf("%d days: %d, want %d", apart, got, want)
		}
	}
}

func TestMatchesOrderByScore(t *testing.T) {
	weak := doc(func(d *sources.ReceiptDoc) {
		d.ID, d.Title, d.Correspondent, d.Content = 1, "Sonstiges", "Other", "1,00"
	})
	strong := doc(func(d *sources.ReceiptDoc) { d.ID = 2 })
	far := doc(func(d *sources.ReceiptDoc) { d.ID, d.Day = 3, "2026-10-01" })
	got := matcher{mapping: mapped}.Matches([]sources.ReceiptExpense{expense(nil)}, []sources.ReceiptDoc{weak, strong, far})
	if len(got) != 1 || len(got[0].Candidates) != 1 || got[0].Candidates[0].Doc.ID != 2 || got[0].Combos != nil {
		t.Fatalf("matches: %+v", got)
	}
}

func TestFilters(t *testing.T) {
	linked := expense(func(e *sources.ReceiptExpense) { e.Key, e.Custom[1] = "1", "https://pl/documents/1/" })
	open := expense(func(e *sources.ReceiptExpense) { e.Key = "2" })
	if got := mapped.unlinkedExpenses([]sources.ReceiptExpense{linked, open}); len(got) != 1 || got[0].Key != "2" {
		t.Fatalf("expenses: %+v", got)
	}
	docs := []sources.ReceiptDoc{
		doc(func(d *sources.ReceiptDoc) { d.ID, d.Custom = 1, map[int64]any{2: "EX-001"} }),
		doc(func(d *sources.ReceiptDoc) { d.ID, d.Custom = 2, map[int64]any{2: ""} }),
		doc(func(d *sources.ReceiptDoc) { d.ID = 3 }),
	}
	if got := mapped.unlinkedDocs(docs); len(got) != 2 || got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("docs: %+v", got)
	}
	years := []sources.ReceiptExpense{
		expense(func(e *sources.ReceiptExpense) { e.Key, e.Day = "1", "2026-01-01" }),
		expense(func(e *sources.ReceiptExpense) { e.Key, e.Day = "2", "2025-12-31" }),
		expense(func(e *sources.ReceiptExpense) { e.Key, e.Day = "3", "" }),
	}
	if got := inYear(years, 2026); len(got) != 1 || got[0].Key != "1" {
		t.Fatalf("year: %+v", got)
	}
}

var withAmount = Mapping{InvoiceSlot: 1, LinkSlot: 2, FieldInvoice: 1, FieldExpense: 2, FieldLink: 3, FieldAmount: 9}

func TestComboAmount(t *testing.T) {
	x := matcher{mapping: withAmount}
	if v, ok := x.comboAmount(doc(func(d *sources.ReceiptDoc) { d.Custom[9], d.Content = "12,50", "99,00 EUR" })); !ok || v != 12.5 {
		t.Fatalf("field first: %v", v)
	}
	x.mapping.FieldAmount = 0
	if _, ok := x.comboAmount(doc(func(d *sources.ReceiptDoc) { d.Content = "Zwischensumme 10,00 Gesamt 42,50" })); ok {
		t.Fatal("two amounts in the text are ambiguous")
	}
}

func TestComboSumsTwoScans(t *testing.T) {
	x := matcher{mapping: withAmount}
	part := func(id int64, title string, amount float64) sources.ReceiptDoc {
		return doc(func(d *sources.ReceiptDoc) { d.ID, d.Title, d.Content, d.Custom[9] = id, title, "", amount })
	}
	scans := []sources.ReceiptDoc{part(1, "Teil A", 20), part(2, "Teil B", 22.5), part(3, "Unrelated", 5)}
	got := x.Matches([]sources.ReceiptExpense{expense(nil)}, scans)
	if len(got[0].Combos) == 0 {
		t.Fatal("no combo")
	}
	c := got[0].Combos[0]
	if c.IDs() != "2,1" || c.Sum != 42.5 || c.Score < minScore || c.Factors[0].Text != "receipts.why_combo_amount" {
		t.Fatalf("combo: %+v", c)
	}
	// The parts are no singles of their own any more, only folded away.
	for _, cand := range got[0].Candidates {
		if cand.Doc.ID == 1 || cand.Doc.ID == 2 {
			t.Fatalf("part %d still a single", cand.Doc.ID)
		}
	}
	if len(got[0].Covered) == 0 {
		t.Fatal("covered singles lost")
	}

	// An exact single leaves no room for combos.
	whole := doc(func(d *sources.ReceiptDoc) { d.ID, d.Custom[9] = 4, 42.5 })
	if got := x.Matches([]sources.ReceiptExpense{expense(nil)}, append(scans, whole)); got[0].Combos != nil {
		t.Fatal("combo next to an exact single")
	}
}

// TestSureMatch: an exact amount with a high score and no close second
// counts as sure; a close second or a combo does not.
func TestSureMatch(t *testing.T) {
	x := matcher{mapping: mapped}
	e := expense(func(e *sources.ReceiptExpense) { e.Custom[0] = "RE-99" })
	got := x.Matches([]sources.ReceiptExpense{e}, []sources.ReceiptDoc{doc(nil)})
	if !got[0].Sure() {
		t.Fatalf("not sure: %+v", got[0].Candidates)
	}
	twin := doc(func(d *sources.ReceiptDoc) { d.ID = 11 })
	if got := x.Matches([]sources.ReceiptExpense{e}, []sources.ReceiptDoc{doc(nil), twin}); got[0].Sure() {
		t.Fatal("two equal scans counted as sure")
	}
}

// TestReverseCombo: each part of a 1∶n combo finds the expense as a combo.
func TestReverseCombo(t *testing.T) {
	x := matcher{mapping: withAmount}
	part := func(id int64, amount float64) sources.ReceiptDoc {
		return doc(func(d *sources.ReceiptDoc) { d.ID, d.Content, d.Custom[9] = id, "", amount })
	}
	scans := []sources.ReceiptDoc{part(1, 20), part(2, 22.5)}
	got := x.Reverse(scans, []sources.ReceiptExpense{expense(nil)}, scans)
	for _, m := range got {
		if len(m.Combos) != 1 || m.Combos[0].Expense.Key != "abc" || len(m.Combos[0].Docs) != 2 || len(m.Hits) != 0 {
			t.Fatalf("scan %d: %+v, hits %+v", m.Doc.ID, m.Combos, m.Hits)
		}
	}
}

// TestReverseQueue: a scan finds its expense.
func TestReverseQueue(t *testing.T) {
	other := expense(func(e *sources.ReceiptExpense) { e.Key, e.Amount, e.Vendor = "2", 999, "Else" })
	got := matcher{mapping: mapped}.Reverse([]sources.ReceiptDoc{doc(nil)}, []sources.ReceiptExpense{other, expense(nil)}, nil)
	if len(got[0].Hits) != 1 || got[0].Hits[0].Expense.Key != "abc" {
		t.Fatalf("reverse: %+v", got)
	}
}

// TestAliasLiftsVendor: a learned name makes a poor vendor hit strong.
func TestAliasLiftsVendor(t *testing.T) {
	e := expense(func(e *sources.ReceiptExpense) { e.Vendor = "HO" })
	d := doc(func(d *sources.ReceiptDoc) { d.Title, d.Correspondent = "Beleg", "Hetzner Online GmbH" })
	before := matcher{mapping: mapped}.vendorFactor(e, []sources.ReceiptDoc{d}, false)
	var a Aliases
	a.learn("HO", "Hetzner Online GmbH")
	after := matcher{mapping: mapped, aliases: a}.vendorFactor(e, []sources.ReceiptDoc{d}, false)
	if before.Points != 0 || after.Points != vendorMax || after.Text != "receipts.why_vendor_alias" {
		t.Fatalf("before %+v, after %+v", before, after)
	}
}
