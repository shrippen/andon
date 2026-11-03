// Package bayes is a naive Bayes classifier: it learns labelled examples,
// each a bag of features, and names the likeliest label for a new one.
//
//	learn  "Hetzner Server 11 €"  → EDV-Kosten
//	       "Kagi Suche 4 €"       → EDV-Kosten
//	       "teilAuto Fahrt 148 €" → Reisekosten
//	guess  "Hetzner Cloud 9 €"    → EDV-Kosten (p 0,9)
//
// Features unseen in training are left out; they say nothing about any
// label. Counts are smoothed, so an unseen pairing is unlikely, not impossible.
package bayes

import (
	"math"
	"slices"
)

// smoothing is added to every count (Lidstone). Below Laplace's 1 so a
// word seen with a label outweighs the prior of a frequent other label:
// few examples per line are the rule here.
const smoothing = 0.1

// Model holds what was learned.
type Model struct {
	examples map[string]int            // per label
	counts   map[string]map[string]int // label → feature → times seen
	totals   map[string]int            // label → features seen
	known    map[string]bool           // every feature seen
	n        int                       // examples
}

// Guess is a label with its probability given the features.
type Guess struct {
	Label string
	P     float64
}

// New is an empty model.
func New() *Model {
	return &Model{examples: map[string]int{}, counts: map[string]map[string]int{}, totals: map[string]int{}, known: map[string]bool{}}
}

// Learn adds one example.
func (m *Model) Learn(label string, features []string) {
	if label == "" {
		return
	}
	m.n++
	m.examples[label]++
	if m.counts[label] == nil {
		m.counts[label] = map[string]int{}
	}
	for _, f := range features {
		m.counts[label][f]++
		m.totals[label]++
		m.known[f] = true
	}
}

// Knows says whether f was seen in training.
func (m *Model) Knows(f string) bool { return m.known[f] }

// Rank is every label, likeliest first; nil before any example or
// without a known feature (the prior alone proposes nothing).
func (m *Model) Rank(features []string) []Guess {
	var seen []string
	for _, f := range features {
		if m.known[f] {
			seen = append(seen, f)
		}
	}
	if m.n == 0 || len(seen) == 0 {
		return nil
	}

	// log P(label) + Σ log P(feature | label)
	vocab := float64(len(m.known))
	logs := map[string]float64{}
	top := math.Inf(-1)
	for label, n := range m.examples {
		l := math.Log(float64(n) / float64(m.n))
		for _, f := range seen {
			l += math.Log((float64(m.counts[label][f]) + smoothing) / (float64(m.totals[label]) + smoothing*vocab))
		}
		logs[label], top = l, max(top, l)
	}

	// Normalise via the largest term (no underflow on many features).
	var sum float64
	for _, l := range logs {
		sum += math.Exp(l - top)
	}
	out := make([]Guess, 0, len(logs))
	for label, l := range logs {
		out = append(out, Guess{Label: label, P: math.Exp(l-top) / sum})
	}
	slices.SortFunc(out, func(a, b Guess) int {
		if a.P != b.P {
			return cmpDesc(a.P, b.P)
		}
		return compareStr(a.Label, b.Label)
	})
	return out
}

// Best is the likeliest label if its probability reaches min, else none.
func (m *Model) Best(features []string, min float64) (Guess, bool) {
	r := m.Rank(features)
	if len(r) == 0 || r[0].P < min {
		return Guess{}, false
	}
	return r[0], true
}

func cmpDesc(a, b float64) int {
	if a > b {
		return -1
	}
	return 1
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
