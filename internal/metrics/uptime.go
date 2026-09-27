package metrics

// Uptime as Andon sees it: every analysis run (5 min) counts, per
// monitor and day, how often it ran and how often the monitor was up.
//
//	kuma.runs.nas  2026-09-27  288
//	kuma.up.nas    2026-09-27  280   → 97,2 %

import (
	"time"

	"andon/internal/sources"
)

// Tallies are this run's counts, added to the day's (see data.AddSamples).
func Tallies(datasets map[string]any) map[string]float64 {
	out := map[string]float64{}
	for _, raw := range datasets {
		kuma, ok := raw.(*sources.KumaDataset)
		if !ok {
			continue
		}
		for _, m := range kuma.Monitors {
			up := 0.0
			if m.Status == sources.KumaUp {
				up = 1
			}
			out[key("kuma", "runs", m.Name)] = 1
			out[key("kuma", "up", m.Name)] = up
		}
	}
	return out
}

// dayTotals sums a series per day.
func dayTotals(points []Point) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, p := range points {
		out[Today(p.Day)] += p.Value
	}
	return out
}

// UptimeDays is the share of runs a monitor was up on each of the last
// n days, oldest first; -1 for a day without runs.
func UptimeDays(h *History, monitor string, now time.Time, n int) []float64 {
	runs := dayTotals(h.SeriesOf(key("kuma", "runs", monitor)))
	ups := dayTotals(h.SeriesOf(key("kuma", "up", monitor)))
	out := make([]float64, n)
	for i := range out {
		day := Today(now).AddDate(0, 0, i-n+1)
		out[i] = -1
		if runs[day] > 0 {
			out[i] = ups[day] / runs[day]
		}
	}
	return out
}

// Uptime is a monitor's share of up runs from a day on; ok=false without runs.
func Uptime(h *History, monitor string, from, now time.Time) (float64, bool) {
	var runs, ups float64
	for day, n := range dayTotals(h.SeriesOf(key("kuma", "runs", monitor))) {
		if !day.Before(Today(from)) && !day.After(Today(now)) {
			runs += n
		}
	}
	for day, n := range dayTotals(h.SeriesOf(key("kuma", "up", monitor))) {
		if !day.Before(Today(from)) && !day.After(Today(now)) {
			ups += n
		}
	}
	if runs == 0 {
		return 0, false
	}
	return ups / runs, true
}

// FirstRun is the first day from which a monitor has recorded runs, zero
// when it has none.
func FirstRun(h *History, monitor string, from time.Time) time.Time {
	var first time.Time
	for day, n := range dayTotals(h.SeriesOf(key("kuma", "runs", monitor))) {
		if n > 0 && !day.Before(Today(from)) && (first.IsZero() || day.Before(first)) {
			first = day
		}
	}
	return first
}
