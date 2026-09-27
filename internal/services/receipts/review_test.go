package receipts

import (
	"errors"
	"testing"

	"andon/internal/sources"
)

// A scan that already names another expense must not be linked again:
// the old expense would keep pointing at it, one-sided.
func TestLinkRefusesScanOfOtherExpense(t *testing.T) {
	e := expense(func(e *sources.ReceiptExpense) { e.Number = "EX-0041" })
	taken := doc(func(d *sources.ReceiptDoc) { d.Custom[mapped.FieldExpense] = "EX-0042" })
	var other LinkedElsewhere
	if err := checkFree(mapped, e, []sources.ReceiptDoc{taken}); !errors.As(err, &other) || other.Number != "EX-0042" {
		t.Fatalf("taken scan: %v", err)
	}
	same := doc(func(d *sources.ReceiptDoc) { d.Custom[mapped.FieldExpense] = "EX-0041" })
	if err := checkFree(mapped, e, []sources.ReceiptDoc{same, doc(nil)}); err != nil {
		t.Fatalf("own or free scans: %v", err)
	}
}

// Without an invoice number the invoice factor does not count: it says so,
// scores nothing and the score is taken of the reachable points, so a
// perfect amount, date and vendor still reach 100.
func TestScoreWithoutInvoiceNumber(t *testing.T) {
	x := matcher{mapping: mapped}
	c := x.score(expense(nil), doc(func(d *sources.ReceiptDoc) { d.Day = "2026-08-01" }))
	inv := c.Factors[3]
	if !inv.Off || inv.Text != "receipts.why_invoice_none" || inv.Points != 0 {
		t.Fatalf("invoice factor: %+v", inv)
	}
	if c.Score != full {
		t.Fatalf("score %d, want %d", c.Score, full)
	}

	// The expense's own number on the scan still counts, as a bonus.
	c = x.score(expense(nil), doc(func(d *sources.ReceiptDoc) { d.Title = "Beleg zu EX-001" }))
	if inv := c.Factors[3]; inv.Off || !inv.Hit {
		t.Fatalf("own number found: %+v", inv)
	}
}

// One field for two parts of the link would overwrite itself.
func TestMappingTwice(t *testing.T) {
	if mapped.twice() {
		t.Fatal("distinct fields flagged")
	}
	for _, m := range []Mapping{
		{InvoiceSlot: 2, LinkSlot: 2, FieldInvoice: 1, FieldExpense: 2, FieldLink: 3},
		{InvoiceSlot: 1, LinkSlot: 2, FieldInvoice: 2, FieldExpense: 2, FieldLink: 3},
		{InvoiceSlot: 1, LinkSlot: 2, FieldInvoice: 1, FieldExpense: 2, FieldLink: 3, FieldAmount: 3},
	} {
		if !m.twice() {
			t.Errorf("not flagged: %+v", m)
		}
	}
}

// Types that do not fit the part get a warning each.
func TestFieldWarnings(t *testing.T) {
	fields := []sources.DocField{{ID: 1, Name: "Nr", Type: "string"}, {ID: 2, Name: "Ausgabe", Type: "string"},
		{ID: 3, Name: "Link", Type: "string"}, {ID: 4, Name: "Betrag", Type: "date"}}
	m := mapped
	m.FieldAmount = 4
	got := warnings(m, fields)
	if len(got) != 2 || got[0].Key != "receipts.warn_link_type" || got[1].Key != "receipts.warn_amount_type" {
		t.Fatalf("warnings: %+v", got)
	}
}
