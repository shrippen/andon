package vatcheck

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/services/receipts"
	"andon/internal/sources"
)

// TestCaseParts: a new expense counts only the parts Invoice Ninja has
// fields for; it is ready once each stands, a link to a complete
// expense at once.
func TestCaseParts(t *testing.T) {
	c := Case{BankCase: metrics.BankCase{Txn: sources.NinjaBankTxn{Debit: true}, Action: metrics.ActionCreate,
		Draft: metrics.ExpenseDraft{Vendor: "Kabelwerk", CategoryKey: "c1", TaxKnown: true}}, Note: "Stative"}

	c.parts = partsOf(false, false, true)
	if len(c.Parts()) != 8 || c.Done() != 7 || c.Ready() {
		t.Fatalf("without fields: %d parts, %d done, ready %v", len(c.Parts()), c.Done(), c.Ready())
	}
	c.Scans = []receipts.Candidate{{Doc: sources.ReceiptDoc{ID: 7}, Score: 92}}
	if c.Done() != 8 || !c.Ready() {
		t.Fatalf("with a scan: %d done, ready %v", c.Done(), c.Ready())
	}

	c.parts = partsOf(true, true, true)
	if c.Done() != 8 || c.Ready() || (Page{Cases: []Case{c}}).Head().NoEuer != 1 {
		t.Fatalf("with EÜR and number fields: %d done, ready %v", c.Done(), c.Ready())
	}

	c.Expenses = []sources.NinjaExpense{{Key: "e1"}}
	c.Action = metrics.ActionLink
	if !c.Counts() || !c.Ready() {
		t.Fatal("a link to a complete expense is ready")
	}
}

// TestLinkGaps: a link counts the gaps of the expense it takes first
// and offers fields for each expense it may take, per expense.
func TestLinkGaps(t *testing.T) {
	b := book{scanSlot: 2}
	b.cats.EuerSlot = 3
	full := sources.NinjaExpense{Key: "e1", CategoryKey: "c1", Notes: "Stative"}
	full.Custom[1], full.Custom[2] = "https://docs/1", "Arbeitsmittel"
	bare := sources.NinjaExpense{Key: "e2", CategoryKey: "c1", Notes: "Kabel"}

	c := Case{BankCase: metrics.BankCase{Txn: sources.NinjaBankTxn{Debit: true}, Action: metrics.ActionLink,
		Expenses: []sources.NinjaExpense{full, bare}}, parts: partsOf(true, false, true)}
	b.gapped(&c)
	if c.Lacks() || !c.Ready() {
		t.Fatalf("the first expense is complete: gaps %v", c.gaps)
	}
	if !c.Offers("euer") || !c.Offers("scan") || c.Offers("note") || c.GapsOf("e2") != "euer scan" || c.GapsOf("e1") != "" {
		t.Fatalf("offers %v", c.lacks)
	}
}

// TestProposed: a scan any listed case proposes, waiting, apart or a
// gap, gets no receipt case of its own.
func TestProposed(t *testing.T) {
	with := func(id int64) Case {
		return Case{BankCase: metrics.BankCase{Txn: sources.NinjaBankTxn{Debit: true}, Action: metrics.ActionCreate},
			Scans: []receipts.Candidate{{Doc: sources.ReceiptDoc{ID: id}, Score: 100}}, parts: partsOf(false, false, true)}
	}
	p := Page{Cases: []Case{with(1)}, Apart: []Case{with(2)}, Gaps: []Case{with(3)}}
	got := p.proposed()
	if !got[1] || !got[2] || !got[3] {
		t.Fatalf("proposed %v", got)
	}
}
