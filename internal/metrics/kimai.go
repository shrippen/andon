// Package metrics: Kimai — hours, utilization, unbilled work, budgets.
package metrics

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

const (
	minutesPerHour     = 60
	defaultHoursPerDay = 8
)

// Hours selects which timesheet entries count.
type Hours int

const (
	HoursAll Hours = iota
	HoursBillable
)

func sheetDay(s sources.KimaiSheet) (time.Time, bool) { return ParseDay(s.Begin) }

// KimaiFreeDays returns public holidays and approved absences
// (kimai-holiday-bundle), as full (non-half) days off.
func KimaiFreeDays(data *sources.KimaiDataset) map[time.Time]bool {
	days := map[time.Time]bool{}
	for _, h := range data.Holidays {
		if h.HalfDay || h.Date == "" {
			continue
		}
		if d, ok := ParseDay(h.Date); ok {
			days[d] = true
		}
	}
	for d := range AbsentDays(data) {
		days[d] = true
	}
	return days
}

// AbsentDays are the full days of approved absences (vacation, sickness).
func AbsentDays(data *sources.KimaiDataset) map[time.Time]bool {
	days := map[time.Time]bool{}
	for _, a := range data.Absences {
		if a.HalfDay || (a.Status != "" && a.Status != "approved") {
			continue
		}
		for d := range Expand(a.Start, a.End) {
			days[d] = true
		}
	}
	return days
}

// KimaiMinutesBetween sums timesheet minutes in [start, end], optionally
// billable-only.
func KimaiMinutesBetween(data *sources.KimaiDataset, start, end time.Time, kind Hours) int {
	total := 0
	for _, s := range data.Timesheets {
		d, ok := sheetDay(s)
		if !ok || d.Before(start) || d.After(end) {
			continue
		}
		if kind == HoursBillable && !s.Billable {
			continue
		}
		total += s.Minutes
	}
	return total
}

// KimaiValueBetween sums the billable rate of timesheets in [start, end].
func KimaiValueBetween(data *sources.KimaiDataset, start, end time.Time) float64 {
	var total float64
	for _, s := range data.Timesheets {
		d, ok := sheetDay(s)
		if !ok || d.Before(start) || d.After(end) || !s.Billable {
			continue
		}
		total += s.Rate
	}
	return total
}

// KimaiRunning is a running timer plus its elapsed minutes.
type KimaiRunning struct {
	Sheet      sources.KimaiSheet
	RunningMin int
}

// KimaiRunningNow returns every currently active timer with elapsed minutes.
func KimaiRunningNow(data *sources.KimaiDataset, now time.Time) []KimaiRunning {
	var out []KimaiRunning
	for _, s := range data.Active {
		begin, ok := ParseTime(s.Begin)
		if !ok {
			continue
		}
		out = append(out, KimaiRunning{Sheet: s, RunningMin: int(now.Sub(begin).Minutes())})
	}
	return out
}

// KimaiCustomerNames maps customer id to name.
func KimaiCustomerNames(data *sources.KimaiDataset) map[int64]string {
	out := make(map[int64]string, len(data.Customers))
	for _, c := range data.Customers {
		out[c.ID] = c.Name
	}
	return out
}

// KimaiUnbilledGroup is one customer's unbilled work.
type KimaiUnbilledGroup struct {
	CustomerID int64
	Customer   string
	Minutes    int
	Amount     float64
	Oldest     string // "" if none
	AgeDays    int
}

// KimaiUnbilled groups billable, finished, not-yet-exported entries per
// customer (like the abrechnung bundle), oldest-first.
func KimaiUnbilled(data *sources.KimaiDataset, today time.Time) []KimaiUnbilledGroup {
	type agg struct {
		minutes int
		amount  float64
		oldest  *time.Time
	}
	groups := map[int64]*agg{}
	for _, s := range data.Timesheets {
		if !s.Billable || s.Exported || s.End == "" {
			continue
		}
		g, ok := groups[s.CustomerID]
		if !ok {
			g = &agg{}
			groups[s.CustomerID] = g
		}
		g.minutes += s.Minutes
		g.amount += s.Rate
		if d, ok := sheetDay(s); ok && (g.oldest == nil || d.Before(*g.oldest)) {
			g.oldest = &d
		}
	}

	names := KimaiCustomerNames(data)
	out := make([]KimaiUnbilledGroup, 0, len(groups))
	for cid, g := range groups {
		name := names[cid]
		if name == "" {
			name = "?"
		}
		row := KimaiUnbilledGroup{CustomerID: cid, Customer: name, Minutes: g.minutes, Amount: round2(g.amount)}
		if g.oldest != nil {
			row.Oldest = g.oldest.Format("2006-01-02")
			row.AgeDays = int(today.Sub(*g.oldest).Hours() / 24)
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgeDays > out[j].AgeDays })
	return out
}

// KimaiTargetMinutes is the working-time target for [start, end] from the
// Kimai work contract, holidays and approved absences left out; 0 without
// a contract.
func KimaiTargetMinutes(data *sources.KimaiDataset, start, end time.Time) int {
	sum := 0
	for _, d := range KimaiWorkdays(data, start, end) {
		sum += data.Contract.Minutes(d)
	}
	return sum
}

// KimaiWorkdays are the days in [start, end] the contract expects work:
// a target above zero, no holiday, no approved absence. None without a
// contract.
func KimaiWorkdays(data *sources.KimaiDataset, start, end time.Time) []time.Time {
	if data.Contract == nil {
		return nil
	}
	free := KimaiFreeDays(data)
	var days []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if data.Contract.Minutes(d) > 0 && !free[d] {
			days = append(days, d)
		}
	}
	return days
}

// KimaiSummary is the Kimai dashboard's headline numbers.
type KimaiSummary struct {
	TodayMin         int
	WeekMin          int
	MonthMin         int
	BillableMonthMin int
	TargetMonthMin   int
	Utilization      *float64 // nil if there is no target yet
	MonthValue       float64
	Running          []KimaiRunning
	Unbilled         []KimaiUnbilledGroup
}

// KimaiSummaryOf computes KimaiSummary for today.
func KimaiSummaryOf(data *sources.KimaiDataset, today time.Time) KimaiSummary {
	month := MonthStart(today)
	monthMin := KimaiMinutesBetween(data, month, today, HoursAll)
	billableMonth := KimaiMinutesBetween(data, month, today, HoursBillable)
	target := KimaiTargetMinutes(data, month, today)

	var utilization *float64
	if target > 0 {
		u := float64(billableMonth) / float64(target)
		utilization = &u
	}

	return KimaiSummary{
		TodayMin: KimaiMinutesBetween(data, today, today, HoursAll),
		WeekMin:  KimaiMinutesBetween(data, WeekStart(today), today, HoursAll),
		MonthMin: monthMin, BillableMonthMin: billableMonth, TargetMonthMin: target,
		Utilization: utilization, MonthValue: round2(KimaiValueBetween(data, month, today)),
		Running: KimaiRunningNow(data, time.Now().UTC()), Unbilled: KimaiUnbilled(data, today),
	}
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// BudgetUse is how much of a project's budget is used, as a share: the
// higher of money and time when it has both. A monthly budget counts this
// month's timesheets only. ok is false for a project without budget.
func BudgetUse(p sources.KimaiProject, data *sources.KimaiDataset, today time.Time) (share float64, ok bool) {
	money, minutes := p.UsedMoney, p.UsedMinutes
	if p.BudgetType == budgetMonthly {
		money, minutes = 0, 0
		start := MonthStart(today)
		for _, s := range data.Timesheets {
			if d, found := sheetDay(s); s.ProjectID == p.ID && found && !d.Before(start) {
				money += s.Rate
				minutes += s.Minutes
			}
		}
	}
	if p.Budget != 0 {
		share, ok = money/p.Budget, true
	}
	if p.TimeBudgetMin != 0 {
		share, ok = max(share, float64(minutes)/float64(p.TimeBudgetMin)), true
	}
	return share, ok
}

// budgetMonthly is Kimai's budget type of a budget per month.
const budgetMonthly = "month"

// KimaiHourPattern spreads the entries in [start, end] over weekday
// (Monday 0) and hour of day: minutes worked in each slot, in loc.
func KimaiHourPattern(data *sources.KimaiDataset, start, end time.Time, loc *time.Location) [7][24]float64 {
	var out [7][24]float64
	for _, s := range data.Timesheets {
		begin, err := time.Parse(time.RFC3339, s.Begin)
		if err != nil || begin.Before(start) || begin.After(end) {
			continue
		}
		at := begin.In(loc)
		left := float64(s.Minutes)
		for left > 0 {
			hourEnd := at.Truncate(time.Hour).Add(time.Hour)
			part := min(left, hourEnd.Sub(at).Minutes())
			out[(int(at.Weekday())+6)%7][at.Hour()] += part
			left -= part
			at = hourEnd
		}
	}
	return out
}

// The work not yet billed, once a day: a curve that keeps rising shows
// billing falling behind.
func init() {
	Record(func(d *sources.KimaiDataset, now time.Time, r *Readings) {
		total := 0.0
		for _, g := range KimaiUnbilled(d, Today(now)) {
			total += g.Amount
		}
		r.Set(key("kimai", "unbilled"), total)

		// Budget use per project: the curve, not only today's level.
		for _, p := range d.Projects {
			if share, ok := BudgetUse(p, d, Today(now)); ok {
				r.Set(BudgetKey(p.ID), share)
			}
		}
	})
}

// BudgetKey is the series of a project's budget use (share, 1 = used up).
func BudgetKey(projectID int64) string {
	return key("kimai", "budget", strconv.FormatInt(projectID, 10))
}

// UnbookedDays is how far back appointments are checked against Kimai.
const UnbookedDays = 14

// unbookedMatch keeps short names ("IT") from matching every title.
const unbookedMatch = 4

// UnbookedEvents are the appointments of the last UnbookedDays whose title
// names a Kimai customer or project, without any entry for that customer
// on the day: work that may not be booked yet.
func UnbookedEvents(cal *sources.CalendarResult, kimai *sources.KimaiDataset, today time.Time) []sources.Event {
	type target struct {
		name       string
		customerID int64
	}
	var targets []target
	for _, c := range kimai.Customers {
		targets = append(targets, target{c.Name, c.ID})
	}
	for _, p := range kimai.Projects {
		targets = append(targets, target{p.Name, p.CustomerID})
	}
	booked := map[[2]any]bool{}
	for _, s := range kimai.Timesheets {
		if d, ok := ParseDay(s.Begin); ok {
			booked[[2]any{d, s.CustomerID}] = true
		}
	}
	since := today.AddDate(0, 0, -UnbookedDays)
	var out []sources.Event
	for _, e := range cal.Events {
		if e.AllDay || e.Start.Before(since) || !e.Start.Before(today) {
			continue
		}
		title := strings.ToLower(e.Title)
		for _, t := range targets {
			if len(t.name) < unbookedMatch || !strings.Contains(title, strings.ToLower(t.name)) {
				continue
			}
			if !booked[[2]any{Today(e.Start), t.customerID}] {
				out = append(out, e)
			}
			break
		}
	}
	return out
}
