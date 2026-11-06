package metrics

import (
	"hash/fnv"
	"math"
	"slices"
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
//	share     single expenses linked to a booking  percent of it ("50", "whole")
//	note      expenses linked to a booking         private note, hashed ("none")
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
	PurposeShare    = "share"
	PurposeNote     = "note"
)

// Labels of the private model.
const (
	labelPrivate  = "private"
	labelBusiness = "business"
)

// noTaxLabel is an expense without tax.
const noTaxLabel = "none"

// Labels of what most expenses have: the whole booking, no private note.
// They teach the models when not to suggest; they are never offered.
const (
	wholeLabel  = "whole"
	noNoteLabel = "none"
)

// BankLearning is the VAT check's models; a zero Use proposes nothing.
type BankLearning struct {
	Vendor, Private, Category, Tax, Share, Note bayes.Use
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

// LearnShares teaches m how much of a booking its only expense takes.
func LearnShares(m *bayes.Model, n *sources.NinjaDataset) bool {
	if n == nil || !n.BankRead {
		return false
	}
	changed := false
	txns := linkedTxns(n)
	for _, e := range n.Expenses {
		if e.TxnKey == "" {
			changed = m.Forget(e.Key) || changed
			continue
		}
		t, ok := txns[e.Key]
		if !ok {
			continue // its booking is out of the window: keep what was learned
		}
		share, single := bookedShare(n, e, t)
		if !single {
			changed = m.Forget(e.Key) || changed
			continue
		}
		changed = m.Learn(e.Key, shareLabel(share), bankFeatures(t, nil)) || changed
	}
	return changed
}

// shareStep rounds a share to what it was meant as: 51 % is half.
const shareStep = 5.0

// shareLabel is a share as a label: "50", "whole".
func shareLabel(share float64) string {
	step := math.Round(share/shareStep) * shareStep
	if step >= percentWhole || step <= 0 {
		return wholeLabel // above 100: an old booking of the bank's amount as net
	}
	return strconv.FormatFloat(step, 'f', -1, 64)
}

// LearnNotes teaches m which private note a booking's expense carries.
func LearnNotes(m *bayes.Model, n *sources.NinjaDataset) bool {
	return learnExpenses(m, n, func(e sources.NinjaExpense) string { return noteLabel(e.PrivateNote) })
}

// noteLabel names a note by its hash: the model keeps no private text,
// the note comes from the expense that has it, or is gone with it.
func noteLabel(note string) string {
	if note == "" {
		return noNoteLabel
	}
	h := fnv.New64a()
	h.Write([]byte(note))
	return strconv.FormatUint(h.Sum64(), 16)
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

// suggestBut is what u suggests for a booking without the label not
// offered; decided says the model told something, also "none of these".
func suggestBut(u bayes.Use, t sources.NinjaBankTxn, sure *sources.BankTxn, not string) (labels []string, decided bool) {
	for _, g := range u.Suggest(bankFeatures(t, sure), IsWordFeature, maxPicks+1) {
		decided = true
		if g.Label != not {
			labels = append(labels, g.Label)
		}
	}
	return labels[:min(len(labels), maxPicks)], decided
}

// suggestShare is the shares to offer, with the vendor's last expense
// (share "" none) standing in while the model has no answer.
func suggestShare(l BankLearning, t sources.NinjaBankTxn, sure *sources.BankTxn, last string) []Pick {
	labels, decided := suggestBut(l.Share, t, sure, wholeLabel)
	if !decided && last != "" {
		labels = []string{last}
	}
	var out []Pick
	for _, v := range labels {
		out = append(out, Pick{Value: v, Label: v + " %"})
	}
	return out
}

// suggestNote is the private notes to offer, likewise.
func suggestNote(n *sources.NinjaDataset, l BankLearning, t sources.NinjaBankTxn, sure *sources.BankTxn, last string) []Pick {
	labels, decided := suggestBut(l.Note, t, sure, noNoteLabel)
	var out []Pick
	for _, label := range labels {
		i := slices.IndexFunc(n.Expenses, func(e sources.NinjaExpense) bool { return e.PrivateNote != "" && noteLabel(e.PrivateNote) == label })
		if i >= 0 {
			out = append(out, Pick{Value: n.Expenses[i].PrivateNote, Label: n.Expenses[i].PrivateNote})
		}
	}
	if !decided && last != "" {
		out = []Pick{{Value: last, Label: last}}
	}
	return out
}

// SuggestCategory offers an expense without a category one: the model on
// its booking, else what the vendor's last expense has.
func SuggestCategory(n *sources.NinjaDataset, e sources.NinjaExpense, l BankLearning) []Pick {
	var out []Pick
	if t, ok := txnOf(n, e.TxnKey); ok {
		for _, key := range suggest(l.Category, t, nil) {
			if name := categoryName(n, key); name != "" {
				out = append(out, Pick{Value: key, Label: name})
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	if last, ok := lastExpense(n, e.VendorKey); ok {
		if name := categoryName(n, last.CategoryKey); name != "" {
			return []Pick{{Value: last.CategoryKey, Label: name}}
		}
	}
	return nil
}

// SuggestPart is what the models offer a split's part without a scan:
// category and tax by the booking.
func SuggestPart(n *sources.NinjaDataset, t sources.NinjaBankTxn, l BankLearning) DraftSuggest {
	var d ExpenseDraft
	suggestDraft(&d, n, t, nil, l)
	return DraftSuggest{Category: d.Suggest.Category, Tax: d.Suggest.Tax}
}

// suggest is what u suggests for a booking, best first; none without a
// known word of it.
func suggest(u bayes.Use, t sources.NinjaBankTxn, sure *sources.BankTxn) []string {
	var out []string
	for _, g := range u.Suggest(bankFeatures(t, sure), IsWordFeature, maxPicks) {
		out = append(out, g.Label)
	}
	return out
}
