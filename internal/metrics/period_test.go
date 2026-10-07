package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// A period names its span: the last 12 months up to today, this year up
// to today, or last year whole; anything else is the last 12 months.
func TestPeriodSpans(t *testing.T) {
	today := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		raw        string
		start, end string
	}{
		{"", "2025-10-08", "2026-10-07"},
		{"12m", "2025-10-08", "2026-10-07"},
		{"year", "2026-01-01", "2026-10-07"},
		{"prev", "2025-01-01", "2025-12-31"},
		{"bogus", "2025-10-08", "2026-10-07"},
	} {
		s := metrics.PeriodOf(c.raw).Span(today)
		if s.Start.Format(time.DateOnly) != c.start || s.End.Format(time.DateOnly) != c.end {
			t.Errorf("%q: %s – %s, want %s – %s", c.raw, s.Start.Format(time.DateOnly), s.End.Format(time.DateOnly), c.start, c.end)
		}
	}
}

// History starts with the month of the first invoice inside the
// source's window; an old open invoice before it does not extend it. A
// span starting before the history is partial.
func TestNinjaHistoryFrom(t *testing.T) {
	ninja := &sources.NinjaDataset{Since: "2025-01-01", Invoices: []sources.NinjaInvoice{
		{Date: "2023-05-02", Status: "sent", Balance: 10}, {Date: "2025-02-17", Status: "paid"}, {Date: "2026-03-01", Status: "paid"}}}
	from := metrics.NinjaHistoryFrom(ninja)
	if from.Format(time.DateOnly) != "2025-02-01" {
		t.Fatalf("from %s", from.Format(time.DateOnly))
	}
	today := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if !metrics.PeriodPrevYear.Span(today).Partial(from) || metrics.PeriodYear.Span(today).Partial(from) {
		t.Fatal("partial spans")
	}
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{{Begin: "2025-03-04T09:00:00"}, {Begin: "2025-01-20T09:00:00"}}}
	if from := metrics.KimaiHistoryFrom(kimai); from.Format(time.DateOnly) != "2025-01-01" {
		t.Fatalf("kimai from %s", from.Format(time.DateOnly))
	}
}

// A customer card counts hours, projects and revenue of the period asked
// for; unbilled and open amounts stay as of today.
func TestClientCardsPeriod(t *testing.T) {
	today := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme GmbH"}},
		Projects:  []sources.KimaiProject{{ID: 10, Name: "Relaunch", CustomerID: 1}},
		Timesheets: []sources.KimaiSheet{
			{CustomerID: 1, ProjectID: 10, Begin: "2026-09-10T09:00:00", End: "x", Minutes: 120, Rate: 200, Billable: true},
			{CustomerID: 1, ProjectID: 10, Begin: "2025-12-10T09:00:00", End: "x", Minutes: 600, Rate: 1000, Billable: true, Exported: true},
			{CustomerID: 1, ProjectID: 10, Begin: "2025-03-10T09:00:00", End: "x", Minutes: 60, Rate: 100, Billable: true, Exported: true},
		},
	}
	ninja := &sources.NinjaDataset{Clients: []sources.NinjaClient{{ID: 7, Name: "acme gmbh"}}, Invoices: []sources.NinjaInvoice{
		{ClientID: 7, Status: "paid", Date: "2026-03-01", Net: 500},
		{ClientID: 7, Status: "paid", Date: "2025-12-20", Net: 1000},
		{ClientID: 7, Status: "paid", Date: "2025-04-01", Net: 300},
	}}
	for _, c := range []struct {
		period         metrics.Period
		hours, revenue float64
	}{{metrics.PeriodLast12, 12, 1500}, {metrics.PeriodYear, 2, 500}, {metrics.PeriodPrevYear, 11, 1300}} {
		card := metrics.ClientCards(kimai, ninja, today, metrics.CenterMean, nil, c.period.Span(today))[0]
		if card.Hours != c.hours || card.Revenue != c.revenue || card.Unbilled != 200 || card.Projects[0].Hours != c.hours {
			t.Errorf("%s: %+v", c.period, card)
		}
	}
}

// The billing page's sums over a span: invoiced net revenue, payments
// received, net expenses and hours worked.
func TestPeriodSums(t *testing.T) {
	today := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-02-01T09:00:00", End: "x", Minutes: 120}, {Begin: "2025-06-01T09:00:00", End: "x", Minutes: 60}}}
	ninja := &sources.NinjaDataset{
		Invoices: []sources.NinjaInvoice{{Status: "paid", Date: "2026-03-01", Net: 500}, {Status: "paid", Date: "2025-03-01", Net: 300}},
		Payments: []sources.NinjaPayment{{Date: "2026-03-20", Amount: 595}, {Date: "2025-03-20", Amount: 357}},
		Expenses: []sources.NinjaExpense{{Date: "2026-04-01", Amount: 119, Tax: 19}, {Date: "2025-04-01", Amount: 50}},
	}
	got := metrics.PeriodSumsOf(kimai, ninja, metrics.PeriodYear.Span(today))
	if got.Revenue != 500 || got.Paid != 595 || got.Expenses != 100 || got.Hours != 2 {
		t.Fatalf("year: %+v", got)
	}
	got = metrics.PeriodSumsOf(kimai, ninja, metrics.PeriodPrevYear.Span(today))
	if got.Revenue != 300 || got.Paid != 357 || got.Expenses != 50 || got.Hours != 1 {
		t.Fatalf("last year: %+v", got)
	}
	if got := metrics.PeriodSumsOf(nil, nil, metrics.PeriodYear.Span(today)); got.Revenue != 0 {
		t.Fatalf("no data: %+v", got)
	}
}
