package metrics

// Categories across services: which Invoice Ninja expense category a
// Sure category means, and which EÜR line (an expense custom field) a
// Ninja category books to. Both are learned from bookings both sides
// already have; the user confirms (services/verbund, categories.go).
//
//	Sure "Software" ─ shared bookings ─► Ninja "Lizenzen" (12 of 14)
//	Ninja "Lizenzen" ─ its expenses ───► EÜR "Laufende EDV-Kosten" (9)

import (
	"sort"

	"andon/internal/sources"
)

// shareDays is how far apart a Sure booking and its expense may lie.
const shareDays = 5

// Votes counts, per key, how often each answer was seen.
type Votes map[string]map[string]int

func (v Votes) add(key, answer string) {
	if key == "" || answer == "" {
		return
	}
	if v[key] == nil {
		v[key] = map[string]int{}
	}
	v[key][answer]++
}

// Best is a key's most frequent answer and its count, "" if none; ties
// go to the alphabetically first, so the page stays stable.
func (v Votes) Best(key string) (string, int) {
	best, n := "", 0
	answers := make([]string, 0, len(v[key]))
	for a := range v[key] {
		answers = append(answers, a)
	}
	sort.Strings(answers)
	for _, a := range answers {
		if v[key][a] > n {
			best, n = a, v[key][a]
		}
	}
	return best, n
}

// SureCategoryVotes counts, per Sure category, the Ninja categories of
// the expenses its debits paid: same amount, a few days apart.
func SureCategoryVotes(n *sources.NinjaDataset, s *sources.SureDataset) Votes {
	out := Votes{}
	if n == nil || s == nil {
		return out
	}
	for _, t := range s.Transactions {
		if t.Category == "" || t.Amount >= 0 {
			continue
		}
		day, ok := ParseDay(t.Date)
		if !ok {
			continue
		}
		for _, e := range n.Expenses {
			if e.CategoryKey == "" || abs(e.Amount+t.Amount) > amountTol || daysApart(e.Date, day) > shareDays {
				continue
			}
			out.add(t.Category, e.CategoryKey)
			break
		}
	}
	return out
}

// EuerVotes counts, per Ninja category, the values its expenses carry in
// custom field slot (1–4).
func EuerVotes(n *sources.NinjaDataset, slot int) Votes {
	out := Votes{}
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return out
	}
	for _, e := range n.Expenses {
		out.add(e.CategoryKey, e.Custom[slot-1])
	}
	return out
}

// EuerCase is an expense whose EÜR field is empty, with the line its
// category maps to ("" = the category has none yet).
type EuerCase struct {
	Expense sources.NinjaExpense
	Line    string
}

// EuerCases are the expenses from since on with an empty EÜR field in
// slot, newest first.
func EuerCases(n *sources.NinjaDataset, slot int, lines map[string]string, since string) []EuerCase {
	var out []EuerCase
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return nil
	}
	for _, e := range n.Expenses {
		if e.Date < since || e.Custom[slot-1] != "" {
			continue
		}
		out = append(out, EuerCase{Expense: e, Line: lines[e.CategoryKey]})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Expense.Date > out[b].Expense.Date })
	return out
}
