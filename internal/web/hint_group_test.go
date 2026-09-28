package web

import (
	"testing"

	"andon/internal/services/hints"
)

// By client, a client's hints of different rules stand together; hints
// without a client stay grouped by rule.
func TestGroupHintsByClient(t *testing.T) {
	views := []hints.View{
		{ID: 1, Rule: "in.invoice_overdue", Client: "Nivre"},
		{ID: 2, Rule: "in.slow_payer", Client: "Nivre"},
		{ID: 3, Rule: "scrutiny.failing"},
	}
	byRule := groupHints(views, groupRule)
	byClient := groupHints(views, groupClient)
	if len(byRule) != 3 || len(byClient) != 2 || byClient[0].Client != "Nivre" || byClient[0].Count() != 2 || byClient[1].Rule != "scrutiny.failing" {
		t.Fatalf("by rule %d, by client %+v", len(byRule), byClient)
	}
}
