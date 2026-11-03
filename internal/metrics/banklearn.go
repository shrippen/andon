package metrics

import (
	"math"
	"strconv"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// Learned suggestions for an open booking on the VAT check, each its own
// model per Invoice Ninja (services/learn), all reading the booking's
// words: payer, booking text, Sure's merchant.
//
//	purpose   learns from                         label
//	vendor    expenses linked to a booking         vendor key
//	private   bookings of private/mixed accounts   private (archived) | business (booked)
//	category  expenses linked to a booking         category key
//	tax       expenses linked to a booking         rate ("19", "none")
//
// Fixed rules fill: a vendor named in the booking, the vendor's last
// expense, Sure's mapping. A model only suggests, under a field they
// leave empty ("Vorschlag: Technik, Büro"); the user's click fills it.

// Purposes of the VAT check's models.
const (
	PurposeVendor   = "vendor"
	PurposePrivate  = "private"
	PurposeCategory = "category"
	PurposeTax      = "tax"
)

// Labels of the private model.
const (
	labelPrivate  = "private"
	labelBusiness = "business"
)

// noTaxLabel is an expense without tax.
const noTaxLabel = "none"

// BankLearning is the VAT check's models; a zero Use proposes nothing.
type BankLearning struct {
	Vendor, Private, Category, Tax bayes.Use
}

// bankFeatures are a booking's words and order of magnitude.
func bankFeatures(t sources.NinjaBankTxn, sure *sources.BankTxn) []string {
	texts := []string{t.Payer, t.Description}
	if sure != nil {
		texts = append(texts, sure.Merchant, sure.Name)
	}
	var out []string
	for _, w := range words(texts) {
		out = append(out, wordFeature+w)
	}
	if a := math.Abs(t.Amount); a >= 1 {
		out = append(out, amountFeature+strconv.Itoa(int(math.Log2(a))))
	}
	return out
}

// linkedTxns maps each expense linked to a booking to that booking.
func linkedTxns(n *sources.NinjaDataset) map[string]sources.NinjaBankTxn {
	txns := map[string]sources.NinjaBankTxn{}
	for _, t := range n.BankTxns {
		txns[t.Key] = t
	}
	out := map[string]sources.NinjaBankTxn{}
	for _, e := range n.Expenses {
		if t, ok := txns[e.TxnKey]; ok && e.TxnKey != "" {
			out[e.Key] = t
		}
	}
	return out
}

// LearnVendors teaches m which vendor a booking's words name.
func LearnVendors(m *bayes.Model, n *sources.NinjaDataset) bool {
	return learnExpenses(m, n, func(e sources.NinjaExpense) string { return e.VendorKey })
}

// LearnCategories teaches m which category a booking's expense gets.
func LearnCategories(m *bayes.Model, n *sources.NinjaDataset) bool {
	return learnExpenses(m, n, func(e sources.NinjaExpense) string { return e.CategoryKey })
}

// LearnTaxes teaches m which tax rate a booking's expense carries.
func LearnTaxes(m *bayes.Model, n *sources.NinjaDataset) bool {
	return learnExpenses(m, n, taxLabel)
}

// taxLabel is an expense's rate as a label: "19", "none".
func taxLabel(e sources.NinjaExpense) string {
	if e.TaxName == "" && e.TaxRate == 0 {
		return noTaxLabel
	}
	return strconv.FormatFloat(e.TaxRate, 'f', -1, 64)
}

// learnExpenses learns label(expense) from its booking's features; an
// expense whose label was emptied or that was unlinked is forgotten.
func learnExpenses(m *bayes.Model, n *sources.NinjaDataset, label func(sources.NinjaExpense) string) bool {
	if n == nil || !n.BankRead {
		return false // no bookings read: nothing to tell, nothing to forget
	}
	changed := false
	txns := linkedTxns(n)
	for _, e := range n.Expenses {
		l := label(e)
		if e.TxnKey == "" || l == "" { // unlinked or emptied: taken back
			changed = m.Forget(e.Key) || changed
			continue
		}
		t, ok := txns[e.Key]
		if !ok {
			continue // its booking is out of the window: keep what was learned
		}
		changed = m.Learn(e.Key, l, bankFeatures(t, nil)) || changed
	}
	return changed
}

// LearnPrivate teaches m which debits of private and mixed accounts were
// set aside as private (archived) and which were booked (an expense).
func LearnPrivate(m *bayes.Model, n *sources.NinjaDataset) bool {
	if n == nil || !n.BankRead {
		return false
	}
	changed := false
	for _, t := range n.BankTxns {
		if !t.Debit || (t.Role != sources.RolePrivate && t.Role != sources.RoleMixed) {
			continue
		}
		label := ""
		switch {
		case t.Archived:
			label = labelPrivate
		case len(t.ExpenseKeys) > 0:
			label = labelBusiness
		}
		if label == "" {
			changed = m.Forget(t.Key) || changed // open: nothing decided, or taken back
			continue
		}
		changed = m.Learn(t.Key, label, bankFeatures(t, nil)) || changed
	}
	return changed
}

// maxPicks is the most values a suggestion line names.
const maxPicks = 3

// suggest is what u suggests for a booking, best first; none without a
// known word of it.
func suggest(u bayes.Use, t sources.NinjaBankTxn, sure *sources.BankTxn) []string {
	var out []string
	for _, g := range u.Suggest(bankFeatures(t, sure), IsWordFeature, maxPicks) {
		out = append(out, g.Label)
	}
	return out
}
