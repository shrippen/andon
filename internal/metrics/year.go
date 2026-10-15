package metrics

// The year for the tax return (EÜR): what Andon can add up from the
// services, as preparation, not tax advice.
//
//	Invoice Ninja ─ payments, VAT, expenses by EÜR line ─┐
//	Snipe-IT ────── depreciation per asset (its rule) ───┼─► year
//	Kimai, Dawarich ─ days worked at home ───────────────┘

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"andon/internal/sources"
)

// ── Depreciation ──

// AfaRow is one asset's depreciation in a year: linear over its
// Snipe-IT rule's months, by month from the purchase month on.
type AfaRow struct {
	Asset   sources.SnipeAsset
	Months  int     // the rule's months, 0 none
	Year    float64 // depreciation in the year
	Left    float64 // book value at the year's end
	Instant bool    // cost up to GwgLimit: may go off at once (GWG)
}

// GwgLimit is the cost up to which an asset may be expensed at once
// (§ 6 Abs. 2 EStG, net); Snipe-IT's rule still decides the row.
const GwgLimit = 800.0

// Depreciation lists the assets that matter in year: bought in it, or
// still depreciating in it. Assets without cost or date, and those done
// before (or without rule bought earlier), stay out.
func Depreciation(snipe *sources.SnipeDataset, year int) []AfaRow {
	end := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	var out []AfaRow
	for _, a := range snipe.Assets {
		bought, ok := ParseDay(a.PurchaseDate)
		if !ok || a.PurchaseCost <= 0 || bought.After(end) {
			continue
		}
		r := AfaRow{Asset: a, Months: a.DepMonths, Instant: a.PurchaseCost <= GwgLimit, Left: a.PurchaseCost}
		if r.Months > 0 {
			r.Year, r.Left = afaIn(a.PurchaseCost, r.Months, bought, year)
		}
		if r.Year == 0 && bought.Year() != year {
			continue
		}
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b AfaRow) int { return strings.Compare(a.Asset.PurchaseDate, b.Asset.PurchaseDate) })
	return out
}

// afaIn is the depreciation in year and the book value after it: cost
// spread over months, the purchase month counting in full.
//
//	1 200 € over 12 months, bought 2025-09 → 2025: 4 × 100, 2026: 8 × 100
func afaIn(cost float64, months int, bought time.Time, year int) (inYear, left float64) {
	first := bought.Year()*monthsPerYearInt + int(bought.Month()) - 1
	last := first + months - 1
	from, to := year*monthsPerYearInt, year*monthsPerYearInt+monthsPerYearInt-1
	perMonth := cost / float64(months)

	done := max(0, min(last, from-1)-first+1)
	now := max(0, min(last, to)-max(first, from)+1)
	return round2(perMonth * float64(now)), round2(cost - perMonth*float64(done+now))
}

const monthsPerYearInt = 12

// ── Home office ──

// HomeDay is a day worked at home: Kimai has work, no ride took the
// owner out for work.
type HomeDay struct {
	Day     time.Time
	Minutes int
}

// HomeOffice lists the year's days with work in Kimai and no business
// ride or commute (Dawarich); without travel every work day counts and
// Estimated is true.
func HomeOffice(kimai *sources.KimaiDataset, travel *Travel, year int) (days []HomeDay, estimated bool) {
	away := map[time.Time]bool{}
	if travel != nil {
		for _, r := range travel.Rides {
			if r.Class == ClassBusiness || r.Class == ClassCommute {
				away[r.Day()] = true
			}
		}
	}

	worked := map[time.Time]int{}
	for _, s := range kimai.Timesheets {
		d, ok := ParseDay(s.Begin)
		if !ok || d.Year() != year {
			continue
		}
		worked[d] += s.Minutes
	}
	for d, m := range worked {
		if m > 0 && !away[d] {
			days = append(days, HomeDay{Day: d, Minutes: m})
		}
	}
	slices.SortFunc(days, func(a, b HomeDay) int { return a.Day.Compare(b.Day) })
	return days, travel == nil
}

// HomeOfficeAllowance is the flat rate for days (§ 4 Abs. 5 Nr. 6c EStG:
// rate a day, at most maxDays).
func HomeOfficeAllowance(days int, rate float64, maxDays int) float64 {
	return round2(float64(min(days, maxDays)) * rate)
}

// Home office flat rate since 2023: 6 € a day, 210 days.
const (
	HomeOfficeRate    = 6.0
	HomeOfficeMaxDays = 210
)

// ── EÜR lines ──

// EuerLine is one line of the year's expenses: net, input VAT, count.
type EuerLine struct {
	Line     string // "" = no line set
	Net, Tax float64
	Expenses int
}

// EuerLines sums the year's expenses by the EÜR line in custom field
// slot (1–4), biggest first; "" collects those without one.
func EuerLines(ninja *sources.NinjaDataset, slot, year int) []EuerLine {
	byLine := map[string]*EuerLine{}
	for _, e := range ninja.Expenses {
		d, ok := ParseDay(e.Date)
		if !ok || d.Year() != year {
			continue
		}
		line := ""
		if slot >= 1 && slot <= sources.NinjaSlots {
			line = strings.TrimSpace(e.Custom[slot-1])
		}
		l := byLine[line]
		if l == nil {
			l = &EuerLine{Line: line}
			byLine[line] = l
		}
		l.Net += e.Amount - e.Tax
		l.Tax += e.Tax
		l.Expenses++
	}

	out := make([]EuerLine, 0, len(byLine))
	for _, l := range byLine {
		l.Net, l.Tax = round2(l.Net), round2(l.Tax)
		out = append(out, *l)
	}
	slices.SortFunc(out, func(a, b EuerLine) int {
		return cmp.Or(cmp.Compare(b.Net, a.Net), strings.Compare(a.Line, b.Line))
	})
	return out
}

// ── Year in review ──

// YearReview is a year at a glance: hours, revenue, the customers that
// carried it, and the year before where the data reaches back far
// enough.
type YearReview struct {
	Hours, Revenue           float64
	PriorHours, PriorRevenue float64
	PriorHoursKnown          bool // Kimai's data covers the year before
	PriorRevenueKnown        bool // and Invoice Ninja's
	Top                      []YearClient
	New                      []string // clients first invoiced this year
	Busiest                  time.Time // the month with most hours, zero none
}

// YearClient is a client's net revenue in the year.
type YearClient struct {
	Name    string
	Revenue float64
}

// topClients is how many clients the review names.
const topClients = 5

// ReviewYear builds the review; either dataset may be nil.
func ReviewYear(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, year int) YearReview {
	var r YearReview
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	prior := start.AddDate(-1, 0, 0)

	if kimai != nil {
		months := [monthsPerYearInt + 1]float64{}
		earliest := ""
		for _, s := range kimai.Timesheets {
			d, ok := ParseDay(s.Begin)
			if !ok {
				continue
			}
			if day := d.Format(time.DateOnly); earliest == "" || day < earliest {
				earliest = day
			}
			hours := float64(s.Minutes) / minutesPerHour
			switch d.Year() {
			case year:
				r.Hours += hours
				months[d.Month()] += hours
			case year - 1:
				r.PriorHours += hours
			}
		}
		r.PriorHoursKnown = earliest != "" && earliest <= prior.Format(time.DateOnly)
		best := 0
		for m := 1; m <= monthsPerYearInt; m++ {
			if months[m] > months[best] {
				best = m
			}
		}
		if best > 0 {
			r.Busiest = time.Date(year, time.Month(best), 1, 0, 0, 0, 0, time.UTC)
		}
	}

	if ninja != nil {
		names := map[int64]string{}
		for _, c := range ninja.Clients {
			names[c.ID] = c.Name
		}
		byClient, first := map[int64]float64{}, map[int64]int{}
		for _, inv := range NinjaCounted(ninja) {
			d, ok := ParseDay(inv.Date)
			if !ok {
				continue
			}
			if y, seen := first[inv.ClientID]; !seen || d.Year() < y {
				first[inv.ClientID] = d.Year()
			}
			switch d.Year() {
			case year:
				r.Revenue += inv.Net
				byClient[inv.ClientID] += inv.Net
			case year - 1:
				r.PriorRevenue += inv.Net
			}
		}
		r.PriorRevenueKnown = ninja.Since != "" && ninja.Since <= prior.Format(time.DateOnly)
		for id, rev := range byClient {
			r.Top = append(r.Top, YearClient{Name: names[id], Revenue: round2(rev)})
			if first[id] == year && r.PriorRevenueKnown {
				r.New = append(r.New, names[id])
			}
		}
		slices.SortFunc(r.Top, func(a, b YearClient) int { return cmp.Or(cmp.Compare(b.Revenue, a.Revenue), strings.Compare(a.Name, b.Name)) })
		r.Top = r.Top[:min(len(r.Top), topClients)]
		slices.Sort(r.New)
	}
	r.Hours, r.Revenue, r.PriorHours, r.PriorRevenue = round2(r.Hours), round2(r.Revenue), round2(r.PriorHours), round2(r.PriorRevenue)
	return r
}
