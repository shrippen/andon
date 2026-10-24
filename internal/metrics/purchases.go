package metrics

// Purchases made complete (services/purchases): an asset bought in the
// last year should have its cost and warranty in the inventory and an expense
// with its receipt in Invoice Ninja.
//
//	inventory asset ─ cost? warranty? ┐
//	Invoice Ninja ─ expense near the purchase (date, amount) ─ receipt? ─┴─► gaps

import (
	"math"
	"slices"
	"strings"
	"time"

	"andon/internal/sources"
)

// PurchaseGap is what a purchase lacks.
type PurchaseGap string

const (
	GapCost     PurchaseGap = "cost"
	GapWarranty PurchaseGap = "warranty"
	GapExpense  PurchaseGap = "expense"
	GapReceipt  PurchaseGap = "receipt"
)

// PurchaseCase is one asset with gaps.
type PurchaseCase struct {
	Key     string // "p-12"
	Asset   sources.Asset
	Expense *sources.NinjaExpense // the one matched, nil none
	Gaps    []PurchaseGap
}

// Has says whether the case lacks gap.
func (c PurchaseCase) Has(gap PurchaseGap) bool { return slices.Contains(c.Gaps, gap) }

const (
	purchaseDays  = 365  // purchases this recent are checked
	expenseDays   = 14   // an expense this close to the purchase matches
	expenseShare  = 0.02 // and its amount (gross or net) this close
	purchaseNoKey = "p-"
)

// Purchases lists the recent assets with gaps, newest first. ninja may
// be nil (no expense checks); linkSlot is the expense custom field that
// holds the receipt link (1–4, 0 unknown: no receipt check).
func Purchases(inv *sources.InventoryData, ninja *sources.NinjaDataset, linkSlot int, today time.Time) []PurchaseCase {
	var out []PurchaseCase
	for _, a := range inv.Assets {
		bought, ok := ParseDay(a.PurchaseDate)
		if !ok || today.Sub(bought).Hours()/hoursPerDay > purchaseDays || bought.After(today) {
			continue
		}
		c := PurchaseCase{Key: purchaseNoKey + a.ID, Asset: a}
		if a.PurchaseCost <= 0 {
			c.Gaps = append(c.Gaps, GapCost)
		}
		if a.WarrantyExpires == "" {
			c.Gaps = append(c.Gaps, GapWarranty)
		}
		if ninja != nil && a.PurchaseCost > 0 {
			c.Expense = expenseFor(ninja, a.PurchaseCost, bought)
			switch {
			case c.Expense == nil:
				c.Gaps = append(c.Gaps, GapExpense)
			case linkSlot >= 1 && linkSlot <= sources.NinjaSlots && strings.TrimSpace(c.Expense.Custom[linkSlot-1]) == "":
				c.Gaps = append(c.Gaps, GapReceipt)
			}
		}
		if len(c.Gaps) > 0 {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b PurchaseCase) int { return strings.Compare(b.Asset.PurchaseDate, a.Asset.PurchaseDate) })
	return out
}

// expenseFor is the expense nearest the purchase whose gross or net
// amount is the cost.
func expenseFor(ninja *sources.NinjaDataset, cost float64, bought time.Time) *sources.NinjaExpense {
	var best *sources.NinjaExpense
	bestDays := math.MaxFloat64
	for i := range ninja.Expenses {
		e := &ninja.Expenses[i]
		days := math.Abs(daysApart(e.Date, bought))
		if days > expenseDays || days >= bestDays {
			continue
		}
		if near(e.Amount, cost) || near(e.Amount-e.Tax, cost) {
			best, bestDays = e, days
		}
	}
	return best
}

func near(a, b float64) bool { return b > 0 && math.Abs(a-b) <= b*expenseShare }
