package widgets

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

// ── Chart ──

const (
	chartW, chartH, chartPad = 1000.0, 200.0, 10.0
)

// Bar is one chart bar. Geometry (Prev*/Value*) is pre-computed here, not
// in the template, since SVG coordinates must never pick up a locale's
// thousands separator the way money()/num() would apply one.
type Bar struct {
	Label, AxisLabel               string
	Value, Prev                    float64
	PrevX, PrevY, PrevW, PrevH     string
	ValueX, ValueY, ValueW, ValueH string
	LabelX                         string
}

// barSeries is one bar's raw numbers, before geometry is computed.
type barSeries struct {
	Label       string
	Value, Prev float64
}

// barTop is the scale's top: the highest bar, at least extra (a goal line).
func barTop(raw []barSeries, extra float64) float64 {
	top := maxF(1, extra)
	for _, r := range raw {
		top = maxF(top, r.Value, r.Prev)
	}
	return top
}

func barsFrom(raw []barSeries) []Bar { return barsFromTop(raw, barTop(raw, 0)) }

func barsFromTop(raw []barSeries, top float64) []Bar {
	step := (chartW - chartPad) / float64(len(raw))

	bars := make([]Bar, len(raw))
	for i, r := range raw {
		x := chartPad + float64(i)*step
		hp, hv := r.Prev/top*(chartH-chartPad), r.Value/top*(chartH-chartPad)
		bar := Bar{
			Label: r.Label, Value: r.Value, Prev: r.Prev,
			PrevX: fnum(x + step*0.1), PrevY: fnum(chartH - hp), PrevW: fnum(step * 0.35), PrevH: fnum(hp),
			ValueX: fnum(x + step*0.47), ValueY: fnum(chartH - hv), ValueW: fnum(step * 0.4), ValueH: fnum(hv),
			LabelX: fnum(x + step/2),
		}
		if i%2 == 0 && len(r.Label) >= 7 {
			bar.AxisLabel = r.Label[5:] + "/" + r.Label[2:4]
		}
		bars[i] = bar
	}
	return bars
}

func maxF(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func fnum(f float64) string { return fmt.Sprintf("%.1f", f) }

// monthsPerYear splits the year's goal into months.
const monthsPerYear = 12

// chartOptions adds the tile's display choices to a chart view.
func chartOptions(out map[string]any, cfg ChartConfig) map[string]any {
	out["ShowPrev"], out["Values"] = cfg.ShowPrev, cfg.Values
	return out
}

func chartView(cfg ChartConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["data"]
	if !ok || data == nil {
		return map[string]any{}
	}
	today := todayOf(ctx)
	service := enums.ServiceType(ctx.Service)

	switch {
	case cfg.Chart == ChartRevenue && service == enums.ServiceInvoiceNinja:
		var raw []barSeries
		for _, m := range metrics.NinjaByMonth(data.(*sources.NinjaDataset), today, cfg.Months) {
			raw = append(raw, barSeries{m.Month, m.Net, m.Prev})
		}
		out := map[string]any{"Bars": barsFrom(raw), "Unit": "money"}
		if goal := settingsFloat(settingsMap(ctx.Settings, "goals"), "revenue_year", 0); cfg.GoalLine && goal > 0 {
			monthly := goal / monthsPerYear
			out["GoalY"], out["Goal"] = fnum(chartH-monthly/barTop(raw, monthly)*(chartH-chartPad)), monthly
			out["Bars"] = barsFromTop(raw, barTop(raw, monthly))
		}
		return chartOptions(out, cfg)

	case cfg.Chart == ChartSeason && service == enums.ServiceInvoiceNinja:
		var raw []barSeries
		for _, m := range metrics.NinjaSeasonal(data.(*sources.NinjaDataset), today, cfg.Months) {
			raw = append(raw, barSeries{m.Month, m.Net, m.Prev})
		}
		return chartOptions(map[string]any{"Bars": barsFrom(raw), "Unit": "money", "PrevKey": "chart.season_avg"}, cfg)

	case cfg.Chart == ChartHours && service == enums.ServiceKimai:
		cur, prev := kimaiMonthHours(data.(*sources.KimaiDataset), today, cfg.Months)
		var raw []barSeries
		for i := range cur {
			month := metrics.AddMonths(today, i-len(cur)+1).Format("2006-01")
			raw = append(raw, barSeries{month, cur[i], prev[i]})
		}
		return chartOptions(map[string]any{"Bars": barsFrom(raw), "Unit": "hours"}, cfg)
	}
	return map[string]any{"Unsupported": true}
}

// ── Progress (budgets + revenue goal) ──

// ProgressItem is one meter: either a Kimai budget or the revenue-goal bar.
type ProgressItem struct {
	LabelKey string // "" if Label is used instead
	Label    string
	Pct      float64
	HasGoal  bool
	Value    float64
	Goal     float64

	// Drawn bar, in percent of its width: the filled part, the part past
	// 100 % (an exceeded goal), and where the calendar says it should be
	// today (0 = no mark).
	Fill, Over, Soll float64
	SollPct          float64 // share of the period passed (0..1), for the caption
	Series           string  // its recorded history (metrics key), "" = none
	Tier             string  // green, yellow, red
}

// progressSlack is how far a meter may run ahead of (budget) or behind
// (goal) the calendar before it turns yellow.
const progressSlack = 0.1

// Meter kinds: a budget should not run ahead, a goal should not lag.
type meterKind int

const (
	meterBudget meterKind = iota
	meterGoal
)

// budgetLimits are kimai.budget_burn's shares: yellow from warn, red from
// critical.
type budgetLimits struct{ warn, critical float64 }

// budgetLimitsOf reads them from the space's rule settings.
func budgetLimitsOf(ctx ViewCtx) budgetLimits {
	return budgetLimits{rules.Setting(ctx.Settings, "kimai.budget_burn", "warn"), rules.Setting(ctx.Settings, "kimai.budget_burn", "critical")}
}

// shape fills in the drawn bar and its colour from Pct and the share of
// the period that has passed (soll, 0 if unknown).
func (p *ProgressItem) shape(kind meterKind, soll, slack float64, limits budgetLimits) {
	scale := max(p.Pct, 1)
	p.Fill = min(p.Pct, 1) / scale * pctFull
	p.Over = max(p.Pct-1, 0) / scale * pctFull
	p.Soll = soll / scale * pctFull
	p.SollPct = soll

	switch {
	case kind == meterGoal && (p.Pct >= 1 || p.Pct >= soll-slack):
		p.Tier = "green"
	case kind == meterGoal:
		p.Tier = "yellow"
	case p.Pct >= limits.critical:
		p.Tier = "red"
	case soll > 0 && p.Pct > soll+slack, soll == 0 && p.Pct >= limits.warn:
		p.Tier = "yellow"
	default:
		p.Tier = "green"
	}
}

// passed is the share of the period around today that is over (today counted).
func passed(today time.Time, start, end time.Time) float64 {
	return float64(today.Sub(start).Hours()/24+1) / float64(end.Sub(start).Hours()/24)
}

func progressView(cfg ProgressConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["data"]
	if !ok || data == nil {
		return map[string]any{}
	}
	today := todayOf(ctx)
	var items []ProgressItem

	if enums.ServiceType(ctx.Service) == enums.ServiceKimai {
		for _, b := range kimaiBudgets(data.(*sources.KimaiDataset), today) {
			if len(cfg.Projects) > 0 && !slices.Contains(cfg.Projects, strings.ToLower(b.Name)) {
				continue
			}
			item := ProgressItem{Label: b.Name, Pct: b.Pct, Series: metrics.BudgetKey(b.ID)}
			soll := 0.0
			if b.Monthly {
				start := metrics.MonthStart(today)
				soll = passed(today, start, metrics.AddMonths(start, 1))
			}
			item.shape(meterBudget, soll, cfg.Warn, budgetLimitsOf(ctx))
			if !cfg.Soll {
				item.Soll, item.SollPct = 0, 0
			}
			items = append(items, item)
		}
	}
	goal := settingsFloat(settingsMap(ctx.Settings, "goals"), "revenue_year", 0)
	if enums.ServiceType(ctx.Service) == enums.ServiceInvoiceNinja && cfg.Goal && goal > 0 {
		ytd := metrics.NinjaSummaryOf(data.(*sources.NinjaDataset), today, "", "").RevenueYTD
		item := ProgressItem{LabelKey: "progress.revenue_goal", Pct: ytd / goal, HasGoal: true, Value: ytd, Goal: goal}
		start := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		item.shape(meterGoal, passed(today, start, start.AddDate(1, 0, 0)), cfg.Warn, budgetLimitsOf(ctx))
		if !cfg.Soll {
			item.Soll, item.SollPct = 0, 0
		}
		items = append(items, item)
	}
	return map[string]any{"Items": items}
}

// ── Deadlines ──

func deadlinesView(cfg DeadlinesConfig, _ map[string]any, ctx ViewCtx) map[string]any {
	tax, configured := metrics.ParseTaxSettings(ctx.Settings)
	today := todayOf(ctx)

	var items []map[string]any
	if configured {
		shown := map[string]bool{"vat_return": cfg.VAT, "prepayment": cfg.Prepayments, "annual": cfg.Annual}
		for _, d := range metrics.UpcomingDeadlines(tax, today, cfg.Days) {
			if !shown[d.Kind] {
				continue
			}
			item := map[string]any{"Kind": d.Kind, "Due": d.Due.Format("2006-01-02"), "Left": int(d.Due.Sub(today).Hours() / 24),
				"Period": d.Period, "Year": d.Year}
			if cfg.Amounts && d.Amount != nil {
				item["Amount"] = d.Amount
			}
			items = append(items, item)
		}
	}
	return map[string]any{"Items": items, "Configured": configured}
}

// ── Trend ──

const (
	trendWidth  = 1000
	trendHeight = 160
)

func trendView(cfg TrendConfig, results map[string]any, ctx ViewCtx) map[string]any {
	points, _ := results["points"].([][2]any)
	if len(points) < 2 {
		return map[string]any{"Count": len(points)}
	}

	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = asFloat(p[1])
		if cfg.Metric == TrendMonthMinute {
			values[i] /= minutesPerHourInsight
		}
	}
	if cfg.Smooth {
		center := metrics.CenterOf(ctx.Settings)
		smoothed := make([]float64, len(values))
		for i := range values {
			lo, hi := max(i-smoothDays/2, 0), min(i+smoothDays/2+1, len(values))
			smoothed[i] = center.Of(values[lo:hi])
		}
		values = smoothed
	}
	low, high := values[0], values[0]
	for _, v := range values {
		low, high = min(low, v), max(high, v)
	}
	if cfg.Target > 0 {
		low, high = min(low, cfg.Target), max(high, cfg.Target)
	}
	span := high - low
	if span == 0 {
		span = 1
	}
	step := float64(trendWidth) / float64(len(values)-1)

	coords := make([]string, len(values))
	for i, v := range values {
		x := float64(i) * step
		y := trendHeight - (v-low)/span*(trendHeight-10) - 5
		coords[i] = formatPoint(x, y)
	}
	unit := "money"
	if cfg.Metric == TrendMonthMinute {
		unit = "hours"
	}
	return map[string]any{
		"Smooth": cfg.Smooth, "TargetY": targetY(cfg.Target, low, span), "Target": cfg.Target,
		"Path": "M" + joinPoints(coords), "First": points[0][0], "Last": points[len(points)-1][0],
		"Low": low, "High": high, "Now": values[len(values)-1], "Unit": unit, "W": trendWidth, "H": trendHeight,
	}
}

// targetY places a trend's target on its scale; nil without a target.
func targetY(target, low, span float64) any {
	if target <= 0 {
		return nil
	}
	return fnum(trendHeight - (target-low)/span*(trendHeight-10) - 5)
}

func formatPoint(x, y float64) string { return fmt.Sprintf("%.1f,%.1f", x, y) }

func joinPoints(coords []string) string {
	out := coords[0]
	for _, c := range coords[1:] {
		out += " L" + c
	}
	return out
}

// dataName is the query of a tile's own connection dataset.
const dataName = "data"

func dataQuery(any) []Query {
	return []Query{{Name: dataName, Source: "data", Conn: ConnWidget}}
}

// peerKimai names the space's Kimai dataset for rate views.
const peerKimai = "kimai"

var kimaiPeer = Query{Name: peerKimai, Source: "data", Conn: ConnPeer, Service: enums.ServiceKimai}

// peerSure names the space's Sure dataset (recurring costs).
const peerSure = "sure"

var surePeer = Query{Name: peerSure, Source: "data", Conn: ConnPeer, Service: enums.ServiceSure}

func kpiQueries(cfg KpiConfig) []Query {
	switch cfg.Metric {
	case MetricEffectiveRate:
		return append(dataQuery(nil), kimaiPeer)
	case MetricLiquidity30, MetricSafeToSpend:
		return append(dataQuery(nil), surePeer)
	case MetricCash:
		if cfg.Free {
			return append(dataQuery(nil), peer(peerNinja, enums.ServiceInvoiceNinja))
		}
	}
	return dataQuery(nil)
}

func tableQueries(cfg TableConfig) []Query {
	if cfg.Table == TableRates {
		return append(dataQuery(nil), kimaiPeer)
	}
	return append(append(dataQuery(nil), crossQueries(cfg.Table)...), homelabQueries(cfg.Table)...)
}
