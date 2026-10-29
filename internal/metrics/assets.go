package metrics

// An inventory's headline numbers and its next dates, for the kpi and
// table tiles of any member of the inventory category (InventoryOf).

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// AssetValue is what an inventory's things cost together, and how many
// there are.
func AssetValue(inv *sources.InventoryData) (value float64, count int) {
	for _, a := range inv.Assets {
		value += a.PurchaseCost
	}
	return round2(value), len(inv.Assets)
}

// AssetDates are the inventory's dates within `days` (default
// horizonDays), soonest first; those the service calls late stay in view
// however far back.
func AssetDates(inv *sources.InventoryData, today time.Time, days int) []sources.AssetDate {
	if days == 0 {
		days = horizonDays
	}
	var out []sources.AssetDate
	for _, d := range inv.Dates {
		if d.Late || within(d.Date, today, days) {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
