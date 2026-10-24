package caps

import (
	"slices"

	"andon/internal/enums"
)

// Category groups services that keep the same kind of data in one shape
// (sources.InventoryData, sources.BankData): rules, metrics and tiles
// read the shape, never one service (CAPABILITIES.md, "Kategorien").
//
//	Snipe-IT ─┐
//	          ├─► inventory ─► inventory.* rules, depreciation, costs
//	Kept ─────┘
type Category string

// Categories with more than one member.
const (
	Inventory Category = "inventory" // assets with purchase and warranty
	Bank      Category = "bank"      // bank accounts, transactions, recurring payments
)

// categories lists each category's members, the preferred one first: in a
// Verbund with two, the first counts, so nothing is written off or
// summed twice.
var categories = map[Category][]enums.ServiceType{
	Inventory: {enums.ServiceSnipeIT},
	Bank:      {enums.ServiceSure, enums.ServiceFirefly},
}

// Members are the services of a category, the preferred first.
func Members(c Category) []enums.ServiceType {
	return slices.Clone(categories[c])
}

// CategoryOf is the category a service belongs to; false for none.
func CategoryOf(s enums.ServiceType) (Category, bool) {
	for c, members := range categories {
		if slices.Contains(members, s) {
			return c, true
		}
	}
	return "", false
}

// AllCategories lists the categories, sorted.
func AllCategories() []Category {
	out := make([]Category, 0, len(categories))
	for c := range categories {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}
