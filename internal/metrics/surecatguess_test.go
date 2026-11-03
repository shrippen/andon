package metrics

import (
	"testing"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// A merchant without filed bookings gets the category learned from
// similar ones, unticked; a merchant's own votes win; no known word, a
// category Sure no longer has, or no model: no proposal.
func TestUncategorizedLearned(t *testing.T) {
	cats := []sources.BankCategory{{ID: "tech", Name: "Technik"}, {ID: "food", Name: "Essen"}}
	bank := &sources.BankData{Categories: cats, Transactions: []sources.BankTxn{
		{ID: "1", Date: "2026-09-01", Merchant: "Kabelwerk Studiobedarf", Amount: -249, CategoryID: "tech"},
		{ID: "2", Date: "2026-09-02", Merchant: "Thomann Studiobedarf", Amount: -120, CategoryID: "tech"},
		{ID: "3", Date: "2026-09-03", Merchant: "Bäckerei Krume", Amount: -6.4, CategoryID: "food"},
		{ID: "4", Date: "2026-10-01", Merchant: "Lichtwerk Studiobedarf", Amount: -180},
		{ID: "5", Date: "2026-10-02", Merchant: "Bäckerei Krume", Amount: -5.9},
		{ID: "6", Date: "2026-10-03", Merchant: "Zollamt", Amount: -40},
	}}
	l := bayes.Use{Model: bayes.New(), MinP: EuerMinP}

	got := map[string]CatCase{}
	for _, c := range Uncategorized(bank, l) {
		got[c.Merchant] = c
	}
	if c := got["Lichtwerk Studiobedarf"]; c.CategoryID != "tech" || c.From != CatFromLearned || c.Ticked {
		t.Errorf("learned: %+v", c)
	}
	if c := got["Bäckerei Krume"]; c.CategoryID != "food" || c.From != CatFromMerchant || !c.Ticked {
		t.Errorf("votes: %+v", c)
	}
	if c := got["Zollamt"]; c.CategoryID != "" || c.From != CatFromNothing {
		t.Errorf("unknown word: %+v", c)
	}

	// Sure dropped the category: not proposed.
	bank.Categories = cats[1:]
	for _, c := range Uncategorized(bank, l) {
		if c.Merchant == "Lichtwerk Studiobedarf" && c.CategoryID != "" {
			t.Errorf("gone category proposed: %+v", c)
		}
	}
	bank.Categories = cats

	// Switched off: only votes.
	for _, c := range Uncategorized(bank, bayes.Use{}) {
		if c.From == CatFromLearned {
			t.Errorf("off: %+v", c)
		}
	}
}

// Teaching learns filed bookings, forgets one whose category was taken
// back, and reports no change on the same data; CatFiled learns the
// user's filing.
func TestCatLearn(t *testing.T) {
	bank := &sources.BankData{Transactions: []sources.BankTxn{
		{ID: "1", Merchant: "Kabelwerk", Amount: -249, CategoryID: "tech"},
		{ID: "2", Name: "Umbuchung", Amount: -500, CategoryID: "move", Transfer: true},
	}}
	m := bayes.New()
	if !CatLearn(m, bank) || CatLearn(m, bank) || m.Stats().Examples != 1 {
		t.Fatalf("learn: %+v", m.Stats())
	}
	if !m.Knows("w:kabelwerk") || !m.Knows("a:-7") {
		t.Errorf("features: %+v", m.Stats())
	}

	bank.Transactions[0].CategoryID = ""
	if !CatLearn(m, bank) || m.Stats().Examples != 0 {
		t.Errorf("taken back: %+v", m.Stats())
	}

	if !CatFiled(m, bank.Transactions[:1], "tech") || m.Stats().Examples != 1 {
		t.Errorf("filed: %+v", m.Stats())
	}
}

// A learned booking Sure later marks as a transfer is forgotten:
// transfers need no category, so the model must not propose one.
func TestCatLearnTransfer(t *testing.T) {
	bank := &sources.BankData{Transactions: []sources.BankTxn{
		{ID: "1", Merchant: "Tagesgeld", Amount: -500, CategoryID: "savings"},
	}}
	m := bayes.New()
	CatLearn(m, bank)

	bank.Transactions[0].Transfer = true
	if !CatLearn(m, bank) || m.Stats().Examples != 0 {
		t.Errorf("transfer still learned: %+v", m.Stats())
	}
}
