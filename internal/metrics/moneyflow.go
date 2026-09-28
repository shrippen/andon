package metrics

import (
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
