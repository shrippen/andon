package metrics

// Invoice drafts from unbilled Kimai time:
//
//	Kimai customer "Acme GmbH" ── same name ──► Ninja client "acme gmbh"
//	sheets (billable, stopped, not exported) ── per project+activity ──► lines
//	  "Relaunch" · "Entwicklung 03.09.–24.09.2026" · 12.5 h · 95.00

import (
	"math"
	"sort"
	"time"

	"andon/internal/sources"
)

const draftDay = "02.01."

// DraftLine is one invoice position.
type DraftLine struct {
	Product, Notes string
	Hours, Rate    float64
	Amount         float64
}

// Draft is one customer's invoice proposal.
type Draft struct {
	CustomerID int64
	Customer   string
	ClientKey  string // Invoice Ninja client id; "" when no client matches
	Client     string
	Lines      []DraftLine
	SheetIDs   []int64
	Total      float64
	Oldest     time.Time
}

type draftKey struct {
	project  int64
	activity string
}

type draftAgg struct {
	minutes     int
	amount      float64
	hourly      float64 // Kimai's hourly rate if all sheets share one, else 0
	mixed       bool
	first, last time.Time
}

// hoursScale keeps quantities exact to the minute (1/60 h ≈ 0.0167).
const hoursScale = 10000

// rate is the line's hourly rate: Kimai's own, or the amount spread over
// the exact hours when sheets differ or Kimai did not send one.
func (a *draftAgg) rate(hours float64) float64 {
	if a.hourly > 0 && !a.mixed {
		return a.hourly
	}
	if hours == 0 {
		return 0
	}
	return round2(a.amount / hours)
}

// Drafts builds one draft per Kimai customer with unbilled time, oldest
// work first; m ties customers to clients.
func Drafts(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, m ClientMap) []Draft {
	projects := map[int64]string{}
	for _, p := range kimai.Projects {
		projects[p.ID] = p.Name
	}
	names := KimaiCustomerNames(kimai)

	byCustomer := map[int64]map[draftKey]*draftAgg{}
	sheets := map[int64][]int64{}
	for _, s := range kimai.Timesheets {
		if !s.Billable || s.Exported || s.End == "" {
			continue
		}
		day, ok := ParseDay(s.Begin)
		if !ok {
			continue
		}
		if byCustomer[s.CustomerID] == nil {
			byCustomer[s.CustomerID] = map[draftKey]*draftAgg{}
		}
		key := draftKey{s.ProjectID, s.Activity}
		agg := byCustomer[s.CustomerID][key]
		if agg == nil {
			agg = &draftAgg{first: day, last: day}
			byCustomer[s.CustomerID][key] = agg
		}
		agg.minutes += s.Minutes
		agg.amount += s.Rate
		if agg.hourly != 0 && agg.hourly != s.HourlyRate {
			agg.mixed = true
		}
		agg.hourly = s.HourlyRate
		agg.first = minTime(agg.first, day)
		agg.last = maxTime(agg.last, day)
		sheets[s.CustomerID] = append(sheets[s.CustomerID], s.ID)
	}

	var out []Draft
	for cid, groups := range byCustomer {
		d := Draft{CustomerID: cid, Customer: names[cid], SheetIDs: sheets[cid]}
		if c, ok := m.ClientOf(ninja, cid, d.Customer); ok {
			d.ClientKey, d.Client = c.Ref(), c.Name
		}
		for key, agg := range groups {
			// Exact hours and the real rate; only the amount is rounded,
			// as Invoice Ninja does for quantity × cost.
			exact := float64(agg.minutes) / minutesPerHour
			hours := math.Round(exact*hoursScale) / hoursScale
			rate := agg.rate(exact)
			line := DraftLine{Product: projects[key.project], Notes: key.activity + " " + agg.first.Format(draftDay) + "–" + agg.last.Format(draftDay+"2006"),
				Hours: hours, Rate: rate, Amount: round2(hours * rate)}
			d.Lines = append(d.Lines, line)
			d.Total += line.Amount
			if d.Oldest.IsZero() || agg.first.Before(d.Oldest) {
				d.Oldest = agg.first
			}
		}
		sort.Slice(d.Lines, func(a, b int) bool { return d.Lines[a].Product+d.Lines[a].Notes < d.Lines[b].Product+d.Lines[b].Notes })
		d.Total = round2(d.Total)
		out = append(out, d)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Oldest.Before(out[b].Oldest) })
	return out
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
