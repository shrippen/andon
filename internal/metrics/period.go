package metrics

// Periods of the customer and billing pages: each figure counts one span
// and names it, and says when the data begins inside it.
//
//	today 2026-10-07   12m  → 2025-10-08 … 2026-10-07  "last 12 months"
//	                   year → 2026-01-01 … 2026-10-07  "2026"
//	                   prev → 2025-01-01 … 2025-12-31  "2025"

import (
	"time"

	"andon/internal/sources"
)

// Period is the span a page's figures cover.
type Period string

const (
	PeriodLast12   Period = "12m" // the default
	PeriodYear     Period = "year"
	PeriodPrevYear Period = "prev"
)

// Periods is the choice a page offers, in order.
var Periods = []Period{PeriodLast12, PeriodYear, PeriodPrevYear}

// PeriodOf reads a query value; unknown ones are the last 12 months.
func PeriodOf(v string) Period {
	for _, p := range Periods {
		if string(p) == v {
			return p
		}
	}
	return PeriodLast12
}

// Span is a period's first and last day, both counted.
type Span struct {
	Period     Period
	Start, End time.Time
}

// Span is the period around today.
func (p Period) Span(today time.Time) Span {
	today = Today(today)
	switch p {
	case PeriodYear:
		return Span{p, time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC), today}
	case PeriodPrevYear:
		start := time.Date(today.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
		return Span{p, start, start.AddDate(1, 0, -1)}
	}
	return Span{PeriodLast12, today.AddDate(-1, 0, 1), today}
}

// Has tells whether day falls in the span.
func (s Span) Has(day time.Time) bool { return !day.Before(s.Start) && !day.After(s.End) }

// HasDay is Has for an ISO date or timestamp; false when unparsable.
func (s Span) HasDay(value string) bool {
	day, ok := ParseDay(value)
	return ok && s.Has(day)
}

// Partial says the data begins after the span does (from is the first
// day with data; zero = unknown, not partial).
func (s Span) Partial(from time.Time) bool { return !from.IsZero() && from.After(s.Start) }

// NinjaHistoryFrom is the first day of the month of the first invoice
// inside the source's window (Since); an older open invoice, read for
// its balance, does not make the history longer. Zero without invoices
// and window.
func NinjaHistoryFrom(data *sources.NinjaDataset) time.Time {
	var first time.Time
	for _, inv := range data.Invoices {
		day, ok := ParseDay(inv.Date)
		if !ok || (data.Since != "" && inv.Date < data.Since) {
			continue
		}
		if first.IsZero() || day.Before(first) {
			first = day
		}
	}
	if first.IsZero() {
		since, _ := ParseDay(data.Since)
		return since
	}
	return MonthStart(first)
}

// KimaiHistoryFrom is the first day of the month of the first time sheet.
func KimaiHistoryFrom(data *sources.KimaiDataset) time.Time {
	var first time.Time
	for _, s := range data.Timesheets {
		if day, ok := ParseDay(s.Begin); ok && (first.IsZero() || day.Before(first)) {
			first = day
		}
	}
	if first.IsZero() {
		return first
	}
	return MonthStart(first)
}

// PeriodSums is what a span brought: net revenue invoiced, payments
// received, net expenses and hours worked.
type PeriodSums struct {
	Revenue, Paid, Expenses, Hours float64
}

// PeriodSumsOf sums a span; either dataset may be nil.
func PeriodSumsOf(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, span Span) PeriodSums {
	var out PeriodSums
	if kimai != nil {
		out.Hours = round2(float64(KimaiMinutesBetween(kimai, span.Start, span.End, HoursAll)) / minutesPerHour)
	}
	if ninja == nil {
		return out
	}
	out.Revenue = NinjaRevenue(ninja, span.Start, span.End)
	for _, p := range ninja.Payments {
		if span.HasDay(p.Date) {
			out.Paid += p.Amount
		}
	}
	for _, e := range ninja.Expenses {
		if span.HasDay(e.Date) {
			out.Expenses += e.Amount - e.Tax
		}
	}
	out.Paid, out.Expenses = round2(out.Paid), round2(out.Expenses)
	return out
}
