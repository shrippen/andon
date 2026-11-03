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
//	  vendor  v:<key>, words of its name     │
//	  texts   w:<word> (notes, booking)      ▼
//	  amount  a:<log₂ €>             Guess(case) ≥ euerMinP → line
//
// The vendor's last line wins: a vendor mostly keeps its line, and the
// latest choice counts more than the count of old ones.

// EuerFrom says where a proposed line comes from.
type EuerFrom string

const (
	EuerFromNothing EuerFrom = ""
	EuerFromVendor  EuerFrom = "vendor"  // the vendor's last expense
	EuerFromLearned EuerFrom = "learned" // the model
)

// euerMinP is the least probability a learned line is proposed with.
const euerMinP = 0.6

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
	model   *bayes.Model
	names   map[string]string               // vendor key → name
	txns    map[string]sources.NinjaBankTxn // by key
	choices []string                        // a dropdown's values; nil takes any
}

// EuerGuesserOf learns from n's expenses with a line in slot (1–4).
func EuerGuesserOf(n *sources.NinjaDataset, slot int) EuerGuesser {
	g := EuerGuesser{last: VendorEuer(n, slot), model: bayes.New()}
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return g
	}
	g.choices = n.ExpenseFields[slot-1].Options
	g.txns, g.names = map[string]sources.NinjaBankTxn{}, map[string]string{}
	for _, t := range n.BankTxns {
		g.txns[t.Key] = t
	}
	for _, v := range n.Vendors {
		g.names[v.Key] = v.Name
	}

	for _, e := range n.Expenses {
		if line := strings.TrimSpace(e.Custom[slot-1]); line != "" {
			g.model.Learn(line, g.inputOf(e).features())
		}
	}
	return g
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
	if g.model == nil {
		return "", EuerFromNothing
	}
	best, ok := g.model.Best(c.features(), euerMinP)
	if !ok || (len(g.choices) > 0 && !slices.Contains(g.choices, best.Label)) {
		return "", EuerFromNothing
	}
	return best.Label, EuerFromLearned
}

// features are c's words, its vendor and its order of magnitude.
func (c EuerInput) features() []string {
	var out []string
	if c.VendorKey != "" {
		out = append(out, "v:"+c.VendorKey)
	}
	for _, w := range words(append([]string{c.Vendor}, c.Texts...)) {
		out = append(out, "w:"+w)
	}
	if a := math.Abs(c.Amount); a >= 1 {
		out = append(out, "a:"+strconv.Itoa(int(math.Log2(a))))
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
