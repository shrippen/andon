package metrics_test

import (
	"math"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func TestPaymentMoraleRecentSlower(t *testing.T) {
	data := &sources.NinjaDataset{Clients: []sources.NinjaClient{{ID: 1, Name: "Acme"}}}
	// Four invoices paid after 10 days, the last three after 30.
	for i, gap := range []int{10, 10, 10, 30, 30, 30} {
		issued := time.Date(2026, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC)
		data.Invoices = append(data.Invoices, sources.NinjaInvoice{ClientID: 1, Status: "paid", Date: issued.Format("2006-01-02")})
		data.Payments = append(data.Payments, sources.NinjaPayment{ClientID: 1, Date: issued.AddDate(0, 0, gap).Format("2006-01-02")})
	}
	rows := metrics.PaymentMorale(data, 10, metrics.CenterMean)
	if len(rows) != 1 || rows[0].UsualDays != 20 || rows[0].RecentDays != 30 || !rows[0].Worse {
		t.Fatalf("morale: %+v", rows)
	}
}

func TestCashflowEvents(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Number: "7", ClientID: 1, Status: "sent", Date: "2026-09-20", Balance: 1000},
	}}
	sure := &sources.SureDataset{Accounts: []sources.SureAccount{{Type: "depository", Balance: 500}},
		Recurring: []sources.SureRecurring{{Name: "Miete", Status: "active", Expense: true, Amount: 800, Next: "2026-10-01"}}}
	points, events := metrics.Cashflow(metrics.CashInputs{Ninja: ninja, Sure: sure}, today, 30)

	// −800 on 1 Oct, +1000 on 4 Oct (invoice date + 14 days default terms).
	if len(events) != 2 || events[0].Amount != -800 || events[1].Day != time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("events: %+v", events)
	}
	low := points[0].Balance
	for _, p := range points {
		low = min(low, p.Balance)
	}
	if points[0].Balance != 500 || low != -300 || points[len(points)-1].Balance != 700 {
		t.Fatalf("points: start %v low %v end %v", points[0].Balance, low, points[len(points)-1].Balance)
	}
}

// TestCashflowLate: the scenario moves only the picked invoice and keeps
// its number on the event.
func TestCashflowLate(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Number: "7", ClientID: 1, Status: "sent", Date: "2026-09-20", Balance: 1000},
		{Number: "8", ClientID: 1, Status: "sent", Date: "2026-09-20", Balance: 500},
	}}
	_, events := metrics.Cashflow(metrics.CashInputs{Ninja: ninja, LateRef: "7", LateDays: 10}, today, 30)
	days := map[string]time.Time{}
	for _, e := range events {
		days[e.Ref] = e.Day
	}
	if !days["7"].Equal(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)) || !days["8"].Equal(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days: %v", days)
	}
}

// TestPayTerms: a 1000 € invoice paid 20 days after issue against a
// 14-day target is 6 late days; 9 % a year on that and 2 % discount.
func TestPayTerms(t *testing.T) {
	data := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{{ClientID: 1, Status: "paid", Date: "2026-09-01", Amount: 1000}},
		Payments: []sources.NinjaPayment{{ClientID: 1, Date: "2026-09-21"}}}
	p := metrics.NinjaPayTerms(data, 1, 14, time.Time{})
	if p.Revenue != 1000 || p.LateAmounts != 6000 || p.Discount(0.02) != 20 || math.Abs(p.Interest(0.09)-6000*0.09/365) > 1e-9 {
		t.Fatalf("terms: %+v", p)
	}
}
