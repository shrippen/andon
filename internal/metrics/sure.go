package metrics

import (
	"time"

	"andon/internal/sources"
)

// SureDue sums active recurring expenses expected within days.
func SureDue(data *sources.SureDataset, today time.Time, days int) float64 {
	horizon := today.AddDate(0, 0, days)
	var total float64
	for _, r := range data.Recurring {
		next, ok := ParseDay(r.Next)
		if r.Status == "active" && r.Expense && ok && !next.After(horizon) {
			total += r.Amount
		}
	}
	return round2(total)
}

// SureCash is the balance of all bank (depository) accounts.
func SureCash(data *sources.SureDataset) float64 {
	var total float64
	for _, a := range data.Accounts {
		if a.Type == "depository" {
			total += a.Balance
		}
	}
	return round2(total)
}

// SureCashDays is the cash balance at the end of each of the last n days,
// oldest first: today's balance, walked back through the bookings.
func SureCashDays(data *sources.SureDataset, today time.Time, n int) []float64 {
	cash := map[string]bool{}
	for _, a := range data.Accounts {
		if a.Type == "depository" {
			cash[a.Name] = true
		}
	}
	booked := map[string]float64{}
	for _, t := range data.Transactions {
		if cash[t.Account] {
			booked[t.Date] += t.Amount
		}
	}

	out := make([]float64, n)
	balance := SureCash(data)
	for i := n - 1; i >= 0; i-- {
		out[i] = round2(balance)
		balance -= booked[today.AddDate(0, 0, i-n+1).Format(isoDay)]
	}
	return out
}

// SureInfo: net worth for the link tile.
func SureInfo(data *sources.SureDataset) []InfoPart {
	return []InfoPart{part("sure.cash", map[string]any{"amount": map[string]any{"$money": SureCash(data), "currency": data.Currency}})}
}
