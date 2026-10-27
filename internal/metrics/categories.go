package metrics

// Categories across services: which Invoice Ninja expense category a
// Sure category means, and which EÜR line (an expense custom field) a
// Ninja category books to. Both are learned from bookings both sides
// already have; the user confirms (services/verbund, categories.go).
//
//	Sure "Software" ─ shared bookings ─► Ninja "Lizenzen" (12 of 14)
//	Ninja "Lizenzen" ─ its expenses ───► EÜR "Laufende EDV-Kosten" (9)

import (
	"slices"
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
func SureCategoryVotes(n *sources.NinjaDataset, s *sources.BankData) Votes {
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

// VendorEuer is, per vendor, the EÜR line of its latest expense that
// has one (custom field slot 1–4). The line belongs to the expense, not
// its category: one category goes to several lines (EDV, GWG, AfA), a
// vendor mostly to the same.
func VendorEuer(n *sources.NinjaDataset, slot int) map[string]string {
	out := map[string]string{}
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return out
	}
	latest := map[string]string{}
	for _, e := range n.Expenses {
		line := e.Custom[slot-1]
		if e.VendorKey == "" || line == "" || e.Date < latest[e.VendorKey] {
			continue
		}
		latest[e.VendorKey], out[e.VendorKey] = e.Date, line
	}
	return out
}

// EuerChoices are the choices for the field: a dropdown's values, else the
// values expenses already carry, sorted.
func EuerChoices(n *sources.NinjaDataset, slot int) []string {
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return nil
	}
	if f := n.ExpenseFields[slot-1]; len(f.Options) > 0 {
		return f.Options
	}
	var out []string
	for _, e := range n.Expenses {
		if l := e.Custom[slot-1]; l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

// EuerCase is an expense whose EÜR field is empty, with the line its
// vendor's last expense carries ("" = no proposal).
type EuerCase struct {
	Expense sources.NinjaExpense
	Line    string
}

// EuerCases are the expenses from since on with an empty EÜR field in
// slot, newest first.
func EuerCases(n *sources.NinjaDataset, slot int, since string) []EuerCase {
	if n == nil || slot < 1 || slot > sources.NinjaSlots {
		return nil
	}
	lines := VendorEuer(n, slot)
	var out []EuerCase
	for _, e := range n.Expenses {
		if e.Date < since || e.Custom[slot-1] != "" {
			continue
		}
		out = append(out, EuerCase{Expense: e, Line: lines[e.VendorKey]})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Expense.Date > out[b].Expense.Date })
	return out
}
