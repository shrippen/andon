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
