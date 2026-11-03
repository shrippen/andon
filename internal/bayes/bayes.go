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
	"encoding/json"
	"math"
	"slices"
)

// smoothing is added to every count (Lidstone). Below Laplace's 1 so a
// word seen with a label outweighs the prior of a frequent other label:
// few examples per line are the rule here.
const smoothing = 0.1

// Model holds what was learned: the examples by id, and the counts
// built from them.
type Model struct {
	seen map[string]Example

	examples map[string]int            // per label
	counts   map[string]map[string]int // label → feature → times seen
	totals   map[string]int            // label → features seen
	known    map[string]bool           // every feature seen
	n        int                       // examples
}

// Example is one learned case: its label and features.
type Example struct {
	Label    string   `json:"l"`
	Features []string `json:"f"`
}

// Guess is a label with its probability given the features.
type Guess struct {
	Label string
	P     float64
}

// Stats says how much a model knows.
type Stats struct {
	Examples, Labels, Features int
}

// New is an empty model.
func New() *Model {
	m := &Model{seen: map[string]Example{}}
	m.recount()
	return m
}

// Learn takes example id: new, or changed since (a corrected label
// replaces the old one). An empty label is skipped. It says whether the
// model changed.
func (m *Model) Learn(id, label string, features []string) bool {
	if label == "" {
		return false
	}
	if old, ok := m.seen[id]; ok && old.Label == label && slices.Equal(old.Features, features) {
		return false
	}
	if old, ok := m.seen[id]; ok {
		m.count(old, -1)
	}
	e := Example{Label: label, Features: slices.Clone(features)}
	m.seen[id] = e
	m.count(e, 1)
	return true
}

// Forget drops example id; it says whether the model held it.
func (m *Model) Forget(id string) bool {
	old, ok := m.seen[id]
	if !ok {
		return false
	}
	delete(m.seen, id)
	m.count(old, -1)
	return true
}

// Stats counts what the model holds.
func (m *Model) Stats() Stats {
	return Stats{Examples: m.n, Labels: len(m.examples), Features: len(m.known)}
}

// MarshalJSON stores the examples; the counts follow from them.
func (m *Model) MarshalJSON() ([]byte, error) { return json.Marshal(m.seen) }

// UnmarshalJSON reads stored examples.
func (m *Model) UnmarshalJSON(b []byte) error {
	seen := map[string]Example{}
	if err := json.Unmarshal(b, &seen); err != nil {
		return err
	}
	m.seen = seen
	m.recount()
	return nil
}

// recount builds the counts from the examples.
func (m *Model) recount() {
	m.examples, m.counts, m.totals, m.known, m.n = map[string]int{}, map[string]map[string]int{}, map[string]int{}, map[string]bool{}, 0
	for _, e := range m.seen {
		m.count(e, 1)
	}
}

// count adds (by 1) or removes (by -1) one example's counts; a feature
// or label no example holds any more is dropped.
func (m *Model) count(e Example, by int) {
	m.n += by
	m.examples[e.Label] += by
	if m.counts[e.Label] == nil {
		m.counts[e.Label] = map[string]int{}
	}
	for _, f := range e.Features {
		m.counts[e.Label][f] += by
		m.totals[e.Label] += by
		if m.counts[e.Label][f] == 0 {
			delete(m.counts[e.Label], f)
		}
	}
	if m.examples[e.Label] == 0 {
		delete(m.examples, e.Label)
		delete(m.counts, e.Label)
		delete(m.totals, e.Label)
	}
	if by > 0 {
		for _, f := range e.Features {
			m.known[f] = true
		}
		return
	}
	for _, f := range e.Features {
		if !m.holds(f) {
			delete(m.known, f)
		}
	}
}

// holds says whether any label still counts f.
func (m *Model) holds(f string) bool {
	for _, c := range m.counts {
		if c[f] > 0 {
			return true
		}
	}
	return false
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
