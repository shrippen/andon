package metrics

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// EÜR line proposals: the vendor's last line, else what a naive Bayes
// model learned from every expense that has a line.
//
//	expenses with a line ──► features ──► bayes.Model
//	  words   w:<word> (vendor name,         │
//	          notes, booking)                ▼
//	  amount  a:<log₂ €>             Guess(case) ≥ MinP → line
//
// The vendor's last line wins: a vendor mostly keeps its line, and the
// latest choice counts more than the count of old ones; so a vendor key
// would add nothing. Without a known word nothing is proposed: the
// amount alone is no evidence. The model is stored per Invoice Ninja
// (services/learn) and keeps what it learned from deleted expenses.

// EuerFrom says where a proposed line comes from.
type EuerFrom string

const (
	EuerFromNothing EuerFrom = ""
	EuerFromVendor  EuerFrom = "vendor"  // the vendor's last expense
	EuerFromLearned EuerFrom = "learned" // the model
)

// EuerMinP is the least probability a learned line is proposed with,
// unless the instance's settings say otherwise.
const EuerMinP = 0.6

// wordFeature prefixes a word's feature, amountFeature the amount's.
const (
	wordFeature   = "w:"
	amountFeature = "a:"
)

// minWord is the shortest word that counts as a feature.
const minWord = 3

// noiseWords say nothing about a line: legal forms, payment providers'
// filler.
var noiseWords = []string{"gmbh", "mbh", "inc", "ltd", "llc", "und", "the", "for", "für", "von", "co.kg", "kg", "eur", "sepa",
	"lastschrift", "kartenzahlung", "visa", "mastercard", "debit", "payment", "rechnung", "invoice"}

// EuerInput is what a proposal looks at: a booking or an expense.
type EuerInput struct {
	VendorKey, Vendor string
	Texts             []string // notes, payer, booking text
	Amount            float64
}

// EuerGuesser proposes lines for one Invoice Ninja and EÜR field.
type EuerGuesser struct {
	last    map[string]string
	learn   EuerLearning
	names   map[string]string               // vendor key → name
	txns    map[string]sources.NinjaBankTxn // by key
	choices []string                        // a dropdown's values; nil takes any
}

// EuerLearning is the stored model and the least probability a learned
// line needs; a nil Model proposes only the vendor's line.
type EuerLearning struct {
	Model *bayes.Model
	MinP  float64
}

// EuerGuesserOf proposes for n's expenses with a line in slot (1–4);
// l.Model learns n's expenses first (EuerLearn), in memory.
func EuerGuesserOf(n *sources.NinjaDataset, slot int, l EuerLearning) EuerGuesser {
	g := euerReader(n, slot)
	g.learn = l
	if l.Model != nil {
		g.teach(n, slot, l.Model)
	}
	return g
}

// EuerLearn teaches m n's expenses with a line in slot: new ones, and
// ones whose line changed; an emptied line is forgotten, an expense gone
// from n stays learned. It says whether m changed.
func EuerLearn(m *bayes.Model, n *sources.NinjaDataset, slot int) bool {
	return euerReader(n, slot).teach(n, slot, m)
}

// euerReader knows n's vendors and bookings, to read an expense.
func euerReader(n *sources.NinjaDataset, slot int) EuerGuesser {
	g := EuerGuesser{last: VendorEuer(n, slot), txns: map[string]sources.NinjaBankTxn{}, names: map[string]string{}}
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return g
	}
	g.choices = n.ExpenseFields[slot-1].Options
	for _, t := range n.BankTxns {
		g.txns[t.Key] = t
	}
	for _, v := range n.Vendors {
		g.names[v.Key] = v.Name
	}
	return g
}

func (g EuerGuesser) teach(n *sources.NinjaDataset, slot int, m *bayes.Model) bool {
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return false
	}
	changed := false
	for _, e := range n.Expenses {
		line := strings.TrimSpace(e.Custom[slot-1])

		// An emptied line was wrong: forget it, do not propose it back.
		if line == "" {
			changed = m.Forget(e.Key) || changed
			continue
		}
		changed = m.Learn(e.Key, line, g.inputOf(e).features()) || changed
	}
	return changed
}

// inputOf is an expense as the model sees it: its vendor, notes and
// the booking it is linked to.
func (g EuerGuesser) inputOf(e sources.NinjaExpense) EuerInput {
	in := EuerInput{VendorKey: e.VendorKey, Vendor: g.names[e.VendorKey], Texts: []string{e.Notes}, Amount: e.Amount}
	if t, ok := g.txns[e.TxnKey]; ok {
		in.Texts = append(in.Texts, t.Payer, t.Description)
	}
	return in
}

// GuessExpense is Guess for an expense that lacks its line.
func (g EuerGuesser) GuessExpense(e sources.NinjaExpense) (string, EuerFrom) {
	return g.Guess(g.inputOf(e))
}

// Guess is the line for c and where it comes from; "" none.
func (g EuerGuesser) Guess(c EuerInput) (string, EuerFrom) {
	if line := g.last[c.VendorKey]; c.VendorKey != "" && line != "" {
		return line, EuerFromVendor
	}
	m := g.learn.Model
	if m == nil {
		return "", EuerFromNothing
	}
	// Words are the evidence; the amount only weighs in.
	features := c.features()
	if !slices.ContainsFunc(features, func(f string) bool { return strings.HasPrefix(f, wordFeature) && m.Knows(f) }) {
		return "", EuerFromNothing
	}
	best, ok := m.Best(features, g.learn.MinP)
	if !ok || (len(g.choices) > 0 && !slices.Contains(g.choices, best.Label)) {
		return "", EuerFromNothing
	}
	return best.Label, EuerFromLearned
}

// features are c's words, its vendor and its order of magnitude.
func (c EuerInput) features() []string {
	var out []string
	for _, w := range words(append([]string{c.Vendor}, c.Texts...)) {
		out = append(out, wordFeature+w)
	}
	if a := math.Abs(c.Amount); a >= 1 {
		out = append(out, amountFeature+strconv.Itoa(int(math.Log2(a))))
	}
	return out
}

// words are the distinct lower-case words of texts: "AMZN Mktp
// DE*RA8E08NW5" → amzn, mktp; no numbers, codes or noise.
func words(texts []string) []string {
	var out []string
	for _, t := range texts {
		for _, w := range strings.FieldsFunc(strings.ToLower(t), func(r rune) bool { return !unicode.IsLetter(r) && r != '.' }) {
			w = strings.Trim(w, ".")
			if len([]rune(w)) < minWord || slices.Contains(noiseWords, w) || slices.Contains(out, w) || hasDigit(w) {
				continue
			}
			out = append(out, w)
		}
	}
	return out
}

func hasDigit(s string) bool { return strings.ContainsFunc(s, unicode.IsDigit) }
