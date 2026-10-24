package metrics

// Bookings without category in Sure, one case per merchant: the
// merchant's bookings that have one say which (most of them), so the
// rest can follow.
//
//	DKB AG  12× no category ─┐
//	DKB AG   4× "Gebühren" ──┴─► propose "Gebühren" for the 12

import (
	"slices"
	"strings"

	"andon/internal/sources"
)

// CatCase is one merchant's bookings without category.
type CatCase struct {
	Key      string // "m-dkb-ag"
	Merchant string
	Txns     []sources.BankTxn // the ones to file, newest first
	Total    float64           // their sum, signed

	// The proposal: the category most of the merchant's filed bookings
	// have, Votes of Of; "" none.
	CategoryID string
	Votes, Of  int
	Ticked     bool // every filed booking agrees
}

// catPrefix is a case key's prefix.
const catPrefix = "m-"

// Uncategorized lists the merchants with bookings Sure has not filed;
// transfers between own accounts need no category and stay out.
func Uncategorized(bank *sources.BankData) []CatCase {
	byKey := map[string]*CatCase{}
	votes := map[string]map[string]int{}
	var order []string
	for _, t := range bank.Transactions {
		if t.Transfer {
			continue
		}
		name := merchantOf(t)
		key := subKey("", name)
		if key == "" {
			continue
		}
		key = catPrefix + key

		if t.CategoryID != "" {
			if votes[key] == nil {
				votes[key] = map[string]int{}
			}
			votes[key][t.CategoryID]++
			continue
		}
		c := byKey[key]
		if c == nil {
			c = &CatCase{Key: key, Merchant: name}
			byKey[key] = c
			order = append(order, key)
		}
		c.Txns = append(c.Txns, t)
		c.Total += t.Amount
	}

	out := make([]CatCase, 0, len(order))
	for _, key := range order {
		c := *byKey[key]
		slices.SortStableFunc(c.Txns, func(a, b sources.BankTxn) int { return strings.Compare(b.Date, a.Date) })
		for id, n := range votes[key] {
			c.Of += n
			if n > c.Votes || (n == c.Votes && id < c.CategoryID) {
				c.CategoryID, c.Votes = id, n
			}
		}
		c.Ticked = c.Of > 0 && c.Votes == c.Of
		out = append(out, c)
	}

	// Most bookings first: they clean up the most.
	slices.SortStableFunc(out, func(a, b CatCase) int { return len(b.Txns) - len(a.Txns) })
	return out
}

// merchantOf is the booking's merchant, else its name.
func merchantOf(t sources.BankTxn) string {
	if m := strings.TrimSpace(t.Merchant); m != "" {
		return m
	}
	return strings.TrimSpace(t.Name)
}
