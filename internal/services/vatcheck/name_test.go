package vatcheck

import (
	"testing"

	"andon/internal/sources"
)

// A booking without payer (a manual or CSV import in Invoice Ninja) is
// named by its text, not left blank in the review and the journal.
func TestNameFallsBackToText(t *testing.T) {
	var w writer
	p := Planned{Item: Item{Txn: sources.NinjaBankTxn{Description: "Tonerwelt GmbH Toner schwarz"}}}
	if got := w.Name(p); got != "Tonerwelt GmbH Toner schwarz" {
		t.Fatalf("name = %q", got)
	}
	p.Item.Txn.Payer = "Tonerwelt GmbH"
	if got := w.Name(p); got != "Tonerwelt GmbH" {
		t.Fatalf("name with payer = %q", got)
	}
}
