package metrics

import (
	"time"

	"andon/internal/sources"
)

// seasonYears is how many prior years form the seasonal average.
const seasonYears = 3

// NinjaSeasonal returns the trailing months like NinjaByMonth, but Prev is
// the average of the same calendar month over up to three prior years,
// e.g. Sep 2026 vs Ø(Sep 2023, Sep 2024, Sep 2025). Every month averages
// the same years, and only years whose month lies wholly in the history
// (NinjaHistoryFrom): a month before the first invoice is no zero. from
// is where the history begins when it cuts the years short, else zero.
//
//	months 07–09/2026, history from 08/2024 → Ø 2025 alone, from 08/2024
func NinjaSeasonal(data *sources.NinjaDataset, today time.Time, months int) (out []NinjaMonth, from time.Time) {
	out = NinjaByMonth(data, today, months)
	if len(out) == 0 {
		return out, from
	}
	history := NinjaHistoryFrom(data)
	oldest, _ := time.Parse("2006-01", out[0].Month)

	// The years the oldest month has: the later months have them too.
	years := 0
	for back := 1; back <= seasonYears; back++ {
		if history.IsZero() || oldest.AddDate(-back, 0, 0).Before(history) {
			break
		}
		years++
	}
	if years < seasonYears {
		from = history
	}

	for i := range out {
		start, _ := time.Parse("2006-01", out[i].Month)
		var sum float64
		for back := 1; back <= years; back++ {
			prevStart := start.AddDate(-back, 0, 0)
			sum += NinjaRevenue(data, prevStart, AddMonths(prevStart, 1).AddDate(0, 0, -1))
		}
		out[i].Prev = 0
		if years > 0 {
			out[i].Prev = round2(sum / float64(years))
		}
	}
	return out, from
}
