package metrics

// Learned categories for Sure's bookings: a merchant without a filed
// booking to vote (Uncategorized) gets up to three categories a naive
// Bayes model learned from every filed booking, as a suggestion only.
//
//	filed bookings ──► features ──► bayes.Model
//	  words   w:<word> (merchant, name)    │
//	  amount  a:<±log₂ €>                  ▼
//	           Suggest(case), known word → "Vorschlag: Technik, Software"
//
// The amount keeps its sign: an income and an expense of the same size
// rarely share a category. The model is stored per Sure (services/learn).

import (
	"math"
	"slices"
	"strconv"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// catPicks is the most categories suggested for a case; parentSep
// joins a category to its parent.
const (
	catPicks  = 3
	parentSep = " › "
)

// minusSign marks an outgoing amount's feature: "a:-9".
const minusSign = "-"

// CatLearn teaches m bank's filed bookings: new ones, and ones whose
// category changed; a booking whose category was taken back is
// forgotten, as is one marked a transfer since; one gone from bank
// stays learned. It says whether m changed.
func CatLearn(m *bayes.Model, bank *sources.BankData) bool {
	if bank == nil {
		return false
	}
	changed := false
	for _, t := range bank.Transactions {
		// A category taken back was wrong, a transfer needs none: do not
		// propose it again.
		if t.Transfer || t.CategoryID == "" {
			changed = m.Forget(t.ID) || changed
			continue
		}
		changed = m.Learn(t.ID, t.CategoryID, catFeatures(t)) || changed
	}
	return changed
}

// CatFiled teaches m the bookings the user just filed under category.
func CatFiled(m *bayes.Model, txns []sources.BankTxn, category string) bool {
	changed := false
	for _, t := range txns {
		changed = m.Learn(t.ID, category, catFeatures(t)) || changed
	}
	return changed
}

// catSuggest are the learned categories for c, best first, those bank
// still has, named as the select shows them ("Studio › Technik").
func catSuggest(l bayes.Use, c CatCase, bank *sources.BankData) []Pick {
	if len(c.Txns) == 0 {
		return nil
	}

	// Words of every booking; the newest's amount only weighs in.
	var texts []string
	for _, t := range c.Txns {
		texts = append(texts, t.Merchant, t.Name)
	}
	features := append(wordFeatures(texts), catAmount(c.Txns[0].Amount)...)
	var out []Pick
	for _, g := range l.Suggest(features, IsWordFeature, catPicks) {
		i := slices.IndexFunc(bank.Categories, func(k sources.BankCategory) bool { return k.ID == g.Label })
		if i < 0 {
			continue
		}
		k := bank.Categories[i]
		label := k.Name
		if k.Parent != "" {
			label = k.Parent + parentSep + label
		}
		out = append(out, Pick{Value: k.ID, Label: label})
	}
	return out
}

// catFeatures are a booking's words and signed order of magnitude.
func catFeatures(t sources.BankTxn) []string {
	return append(wordFeatures([]string{t.Merchant, t.Name}), catAmount(t.Amount)...)
}

// wordFeatures are texts' words as features.
func wordFeatures(texts []string) []string {
	var out []string
	for _, w := range words(texts) {
		out = append(out, wordFeature+w)
	}
	return out
}

// catAmount is the amount's feature, none below 1 €.
func catAmount(amount float64) []string {
	a := math.Abs(amount)
	if a < 1 {
		return nil
	}
	sign := ""
	if amount < 0 {
		sign = minusSign
	}
	return []string{amountFeature + sign + strconv.Itoa(int(math.Log2(a)))}
}
