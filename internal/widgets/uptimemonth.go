package widgets

// "uptime_month": the month so far per monitor, as Andon measured it
// (every analysis run counts, see metrics.Tallies), worst first, with the
// downtime that share means.

import (
	"sort"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// UptimeRow is one monitor's month.
type UptimeRow struct {
	Name      string
	Share     float64
	SharePct  float64
	DownHours float64
	DownMin   int
	Tier      string // ok, mid, bad (see uptimeOK, uptimeWarn)
}

func uptimeMonthView(_ any, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["data"].(*sources.KumaDataset)
	h, ok2 := results[HistorySlot].(*metrics.History)
	if !ok || !ok2 {
		return map[string]any{}
	}
	today := parseToday(ctx.Today)
	start := metrics.MonthStart(today)
	now := time.Now().UTC()
	if !today.Equal(metrics.Today(now)) {
		now = today.Add(hoursPerDay * time.Hour)
	}

	var rows []UptimeRow
	var since time.Time // earliest measured day, when after the 1st
	for _, m := range data.Monitors {
		share, found := metrics.Uptime(h, m.Name, start, today)
		if !found {
			continue
		}
		// Only the measured time counts: from the first day with runs.
		first := metrics.FirstRun(h, m.Name, start)
		if since.IsZero() || first.Before(since) {
			since = first
		}
		down := (1 - share) * now.Sub(first).Hours()
		row := UptimeRow{Name: m.Name, Share: share, SharePct: share * pctFull, DownHours: down, DownMin: int(down * minutesPerHour), Tier: "bad"}
		switch {
		case share >= uptimeOK:
			row.Tier = "ok"
		case share >= uptimeWarn:
			row.Tier = "mid"
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Share < rows[j].Share })
	out := map[string]any{"Rows": rows, "Month": start.Format("01/2006")}
	if since.After(start) {
		out["Since"] = since.Format(time.DateOnly)
	}
	return out
}

func init() {
	Register(WidgetType{Key: "uptime_month", Decode: decodeEmpty, Template: "widgets/uptime_month", Category: CategoryInsight,
		Service: enums.ServiceUptimeKuma, RefreshS: 1800, Queries: dataQuery, View: uptimeMonthView, Extra: ExtraHistory})
}
