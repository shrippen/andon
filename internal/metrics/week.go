package metrics

// Closing a week in Kimai: work other services saw that Kimai lacks.
//
//	Dawarich visit at a customer (≥ minVisit) ─┐
//	calendar appointment naming a customer   ──┼─► gap, unless Kimai has
//	                                           │   an entry for that
//	                                           │   customer that day
//	the same day and customer twice ───────────┘   → one gap, the
//	                                                 appointment's times
//
// Each gap carries a proposal: the customer's project and activity the
// user booked most lately (WeekProposal).

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/sources"
)

// minVisit is the shortest visit worth an entry, in minutes.
const minVisit = 30

// Gap sources.
const (
	GapVisit = "visit"
	GapEvent = "event"
)

// WeekGap is work the week's evidence shows but Kimai lacks.
type WeekGap struct {
	Key        string // stable: "visit:2026-09-10:12", "event:<hash>"
	Kind       string
	Day        time.Time
	Start, End time.Time
	CustomerID int64
	Title      string // the area's name or the appointment's title
}

// Minutes is the gap's length.
func (g WeekGap) Minutes() int { return int(g.End.Sub(g.Start).Minutes()) }

// WeekGaps lists the gaps of the week starting at start (a Monday),
// oldest first; geo and cal may be nil.
func WeekGaps(kimai *sources.KimaiDataset, geo *sources.DawarichDataset, areas map[string]AreaMapping, cal *sources.CalendarResult, start time.Time, loc *time.Location) []WeekGap {
	if kimai == nil {
		return nil
	}
	end := start.AddDate(0, 0, 7)
	booked := map[[2]any]bool{}
	for _, s := range kimai.Timesheets {
		if d, ok := ParseDay(s.Begin); ok {
			booked[[2]any{d, s.CustomerID}] = true
		}
	}
	found := map[[2]any]WeekGap{}

	// Appointments first: they carry the times.
	if cal != nil {
		for _, e := range cal.Events {
			if e.AllDay || e.Start.Before(start) || !e.Start.Before(end) || e.End.IsZero() {
				continue
			}
			customer := customerNamed(kimai, e.Title)
			day := dayOf(e.Start.In(loc))
			if customer == 0 || booked[[2]any{day, customer}] {
				continue
			}
			k := [2]any{day, customer}
			if _, dup := found[k]; !dup {
				found[k] = WeekGap{Key: GapEvent + ":" + hash(e.Title, e.Start.Format(time.RFC3339)), Kind: GapEvent, Day: day,
					Start: e.Start.In(loc), End: e.End.In(loc), CustomerID: customer, Title: e.Title}
			}
		}
	}
	if geo != nil {
		for _, v := range ClientVisits(geo, areas) {
			if v.Day.Before(start) || !v.Day.Before(end) || v.Minutes < minVisit || booked[[2]any{v.Day, v.CustomerID}] {
				continue
			}
			k := [2]any{v.Day, v.CustomerID}
			if _, dup := found[k]; dup || v.Start == nil {
				continue
			}
			s := v.Start.In(loc)
			found[k] = WeekGap{Key: fmt.Sprintf("%s:%s:%d", GapVisit, v.Day.Format(time.DateOnly), v.CustomerID), Kind: GapVisit,
				Day: v.Day, Start: s, End: s.Add(time.Duration(v.Minutes) * time.Minute), CustomerID: v.CustomerID, Title: v.Area.Name}
		}
	}

	out := make([]WeekGap, 0, len(found))
	for _, g := range found {
		out = append(out, g)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Start.Before(out[b].Start) })
	return out
}

// dayOf is t's calendar day where t is, as the days of Kimai's entries.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// nameMin keeps short names ("IT") from matching every title.
const nameMin = 4

// customerNamed is the customer a title names, or the customer of a
// project it names; 0 if none.
func customerNamed(kimai *sources.KimaiDataset, title string) int64 {
	t := strings.ToLower(title)
	for _, c := range kimai.Customers {
		if len(c.Name) >= nameMin && strings.Contains(t, strings.ToLower(c.Name)) {
			return c.ID
		}
	}
	for _, p := range kimai.Projects {
		if len(p.Name) >= nameMin && strings.Contains(t, strings.ToLower(p.Name)) {
			return p.CustomerID
		}
	}
	return 0
}

func hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum[:6])
}

// WeekProposal is a customer's project and activity name the user
// booked most lately; zero if never.
type WeekProposal struct {
	ProjectID int64
	Activity  string
}

// WeekProposals proposes per customer from the newest entries.
func WeekProposals(kimai *sources.KimaiDataset) map[int64]WeekProposal {
	out := map[int64]WeekProposal{}
	if kimai == nil {
		return out
	}
	sheets := append([]sources.KimaiSheet(nil), kimai.Timesheets...)
	sort.SliceStable(sheets, func(a, b int) bool { return sheets[a].Begin > sheets[b].Begin })
	for _, s := range sheets {
		if _, ok := out[s.CustomerID]; !ok && s.ProjectID > 0 {
			out[s.CustomerID] = WeekProposal{ProjectID: s.ProjectID, Activity: s.Activity}
		}
	}
	return out
}
