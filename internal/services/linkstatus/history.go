package linkstatus

import (
	"time"

	"andon/internal/db"
	"andon/internal/repos/data"
)

// Day is one day of a tile's history.
type Day struct {
	Day   time.Time
	State BarState
	OK    int
	Fail  int
	AvgMs int    // over the successful checks, 0 without one
	Error string // the day's latest failure, "" if none
}

// DownMinutes estimates the time without an answer: one interval per
// failed check.
func (d Day) DownMinutes() int {
	return d.Fail * int(Interval/time.Minute)
}

// DownH and DownM split DownMinutes into hours and minutes.
func (d Day) DownH() int { return d.DownMinutes() / minutesPerHour }
func (d Day) DownM() int { return d.DownMinutes() % minutesPerHour }

const minutesPerHour = 60

// Incident is a run of days with failed checks.
type Incident struct {
	From, To time.Time
	Days     int
	Failed   int
	Error    string // the first day's failure
	Down     bool   // some day without a single answer
}

// DownMinutes estimates the incident's time without an answer.
func (i Incident) DownMinutes() int {
	return i.Failed * int(Interval/time.Minute)
}

// DownH and DownM split DownMinutes into hours and minutes.
func (i Incident) DownH() int { return i.DownMinutes() / minutesPerHour }
func (i Incident) DownM() int { return i.DownMinutes() % minutesPerHour }

// History is a tile's kept days for its detail dialog.
type History struct {
	Days      []Day      // keepDays, oldest first, today last
	Incidents []Incident // newest first
	Share30   float64    // successful checks in the last 30 days, 0–1
	AvgMs30   int
	Checks    int       // all kept checks
	Since     time.Time // first measured day, zero without one
}

// HistoryOf reads a tile's kept days and finds its incidents:
//
//	days   ok ok warn bad warn ok   →   one incident, 3 days, Down
func HistoryOf(q db.Queryer, widgetID int64, today time.Time) History {
	today = today.UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(keepDays - 1))
	rows, _ := data.StatusSince(q, widgetID, start.Format(isoDay))
	byDay := make(map[string]data.DayStatus, len(rows))
	for _, r := range rows {
		byDay[r.Day] = r
	}

	var h History
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		h.Days = append(h.Days, dayOf(d, byDay[d.Format(isoDay)]))
	}
	h.tally(barDays)
	h.Incidents = incidents(h.Days)
	return h
}

// dayOf turns a stored row into a day; a missing row is BarNone.
func dayOf(d time.Time, r data.DayStatus) Day {
	day := Day{Day: d, State: BarNone, OK: r.OK, Fail: r.Fail, Error: r.LastError}
	if r.OK > 0 {
		day.AvgMs = r.MsSum / r.OK
	}
	switch {
	case r.OK+r.Fail == 0:
	case r.Fail == 0:
		day.State = BarUp
	case r.OK == 0:
		day.State = BarDown
	default:
		day.State = BarPartial
	}
	return day
}

// tally sums all checks, the first measured day and the last recent
// days' share and response time.
func (h *History) tally(recent int) {
	good, all, msSum := 0, 0, 0
	for i, d := range h.Days {
		h.Checks += d.OK + d.Fail
		if h.Since.IsZero() && d.State != BarNone {
			h.Since = d.Day
		}
		if i < len(h.Days)-recent {
			continue
		}
		good += d.OK
		all += d.OK + d.Fail
		msSum += d.AvgMs * d.OK
	}
	if all > 0 {
		h.Share30 = float64(good) / float64(all)
	}
	if good > 0 {
		h.AvgMs30 = msSum / good
	}
}

// incidents groups consecutive days with failures, newest first.
func incidents(days []Day) []Incident {
	var out []Incident
	open := false
	for _, d := range days {
		if d.Fail == 0 {
			open = false
			continue
		}
		if !open {
			out = append(out, Incident{From: d.Day, Error: d.Error})
			open = true
		}
		cur := &out[len(out)-1]
		cur.To = d.Day
		cur.Days++
		cur.Failed += d.Fail
		cur.Down = cur.Down || d.State == BarDown
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
