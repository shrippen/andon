package vatcheck

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/services/assist"
	"andon/internal/sources"
)

// Claude's reading, asked for by the user, overrules the learned sort of
// a private account's debit, and the page then says nothing learned.
func TestGuessOverrulesLearned(t *testing.T) {
	b := book{ninja: &sources.NinjaDataset{}}
	debit := sources.NinjaBankTxn{Key: "d1", Debit: true, Role: sources.RoleMixed}

	business := Case{BankCase: metrics.BankCase{Txn: debit, Private: true, Action: metrics.ActionCreate, Learned: true},
		Guess: &assist.Guess{Private: true}}
	b.apply(&business)
	if business.Action != metrics.ActionSkip || business.Learned {
		t.Errorf("Claude private, model business: %s learned %v", business.Action, business.Learned)
	}

	private := Case{BankCase: metrics.BankCase{Txn: debit, Private: true, Action: metrics.ActionSkip, Learned: true},
		Guess: &assist.Guess{Vendor: "Nordhost"}}
	b.apply(&private)
	if private.Action != metrics.ActionCreate || private.Learned {
		t.Errorf("Claude business, model private: %s learned %v", private.Action, private.Learned)
	}

	transfer := Case{BankCase: metrics.BankCase{Txn: debit, Private: true, Action: metrics.ActionSkip, Transfer: &metrics.Transfer{}},
		Guess: &assist.Guess{Vendor: "Nordhost"}}
	b.apply(&transfer)
	if transfer.Action != metrics.ActionSkip {
		t.Errorf("a transfer's side became %s", transfer.Action)
	}
}
