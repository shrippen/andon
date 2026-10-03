package metrics

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// MoneyFlow is the money on its way from work to the account:
//
//	unbilled hours → drafts → sent (not due) → overdue → paid (30 days)
type MoneyFlow struct {
	Unbilled, Drafts, Sent, Overdue, Paid float64
	Currency                              string
}

// paidWindow is how far back payments count as just paid.
const paidWindow = 30

// Total is the money still on its way (everything but paid).
func (f MoneyFlow) Total() float64 { return f.Unbilled + f.Drafts + f.Sent + f.Overdue }

// MoneyFlowOf sums each stage; kimai may be nil (no unbilled stage).
func MoneyFlowOf(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, today time.Time) MoneyFlow {
	f := MoneyFlow{Currency: ninja.Currency}
	if kimai != nil {
		for _, g := range KimaiUnbilled(kimai, today) {
			f.Unbilled += g.Amount
		}
	}
	for _, inv := range ninja.Invoices {
		switch {
		case inv.Status == draftStatus:
			f.Drafts += inv.Amount
		case isOpen(inv.Status) && inv.DueDate != "" && inv.DueDate < today.Format(time.DateOnly):
			f.Overdue += inv.Balance
		case isOpen(inv.Status):
			f.Sent += inv.Balance
		}
	}
	since := today.AddDate(0, 0, -paidWindow).Format(time.DateOnly)
	for _, p := range ninja.Payments {
		if p.Date >= since {
			f.Paid += p.Amount
		}
	}
	return f
}

const draftStatus = "draft"

// FlowDays is how long money takes through the stages: the open work's
// mean age (minutes-weighted) and the mean days from invoice to payment
// over the last year; ok is false where nothing counts.
func FlowDays(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, today time.Time) (work float64, workOK bool, pay float64, payOK bool) {
	if kimai != nil {
		minutes := 0.0
		for _, g := range KimaiUnbilled(kimai, today) {
			work += float64(g.AgeDays) * float64(g.Minutes)
			minutes += float64(g.Minutes)
		}
		if minutes > 0 {
			work, workOK = work/minutes, true
		}
	}
	if ninja != nil {
		n := 0
		for _, gaps := range NinjaPaymentGapsSince(ninja, today.AddDate(-1, 0, 0)) {
			for _, g := range gaps {
				pay += float64(g)
				n++
			}
		}
		if n > 0 {
			pay, payOK = pay/float64(n), true
		}
	}
	return work, workOK, pay, payOK
}

// FixedRate is a fixed-price project's earned rate: its money budget over
// the hours booked on it.
type FixedRate struct {
	Project string
	Budget  float64
	Hours   float64
	Rate    float64
}

// FixedRates lists projects with a money budget for the whole project and
// booked hours, lowest rate first: underestimated work shows at the top.
func FixedRates(kimai *sources.KimaiDataset) []FixedRate {
	var out []FixedRate
	for _, p := range kimai.Projects {
		if p.Budget <= 0 || p.BudgetType == budgetMonthly || p.UsedMinutes <= 0 {
			continue
		}
		hours := float64(p.UsedMinutes) / minutesPerHour
		out = append(out, FixedRate{Project: p.Name, Budget: p.Budget, Hours: hours, Rate: p.Budget / hours})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Rate < out[b].Rate })
	return out
}
