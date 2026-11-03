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

	vendor := Case{BankCase: metrics.BankCase{Txn: debit, Action: metrics.ActionCreate,
		Draft: metrics.ExpenseDraft{Vendor: "Learned GmbH", VendorKey: "v9", VendorFrom: metrics.FromLearned}},
		Guess: &assist.Guess{Vendor: "Nordhost"}}
	b.apply(&vendor)
	if vendor.Draft.Vendor != "Nordhost" || vendor.Draft.VendorKey == "v9" || vendor.Draft.VendorFrom == metrics.FromLearned {
		t.Errorf("learned vendor beat Claude's: %+v", vendor.Draft)
	}

	same := Case{BankCase: metrics.BankCase{Txn: debit, Action: metrics.ActionCreate,
		Draft: metrics.ExpenseDraft{Vendor: "Nordhost", VendorKey: "v2", VendorFrom: metrics.FromLearned, TaxKnown: true, TaxRate: 19, Bill: true}},
		Guess: &assist.Guess{Vendor: "nordhost"}}
	b.apply(&same)
	if same.Draft.VendorKey != "v2" || !same.Draft.TaxKnown || !same.Draft.Bill {
		t.Errorf("same vendor lost its history: %+v", same.Draft)
	}
	mapped := Case{BankCase: metrics.BankCase{Txn: debit, Action: metrics.ActionCreate,
		Draft: metrics.ExpenseDraft{Vendor: "Learned GmbH", VendorKey: "v9", VendorFrom: metrics.FromLearned, Category: "Hosting", CategoryKey: "c2", From: metrics.FromSure}},
		Guess: &assist.Guess{Vendor: "Nordhost"}}
	b.apply(&mapped)
	if mapped.Draft.CategoryKey != "c2" || mapped.Draft.VendorKey == "v9" {
		t.Errorf("Sure's category lost or learned vendor kept: %+v", mapped.Draft)
	}
}
