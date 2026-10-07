package widgets

// Freelance widgets on the Kimai connection:
//
//	kimai_timer   Kimai Lite, see kimailite.go
//	heatmap       hours per day over the last year, GitHub style
//	cashflow      expected balance for the next days (Ninja + Sure + taxes)

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const minutesPerHour = 60

// ── heatmap ──

const (
	heatWeeks = 53
)

// heatLevels are the minute thresholds of levels 1–4 (2 h, 4 h, 6 h, 8 h).
var heatLevels = []int{1, 120, 240, 360, 480}

// HeatCell is one day of the heatmap.
type HeatCell struct {
	Level int
	Day   string
	Hours string
	Goal  string // by_goal: "met", "under" or "" (no work)
}

// HeatConfig is the "heatmap" widget's config.
type HeatConfig struct {
	Months   int
	Weekdays bool // Monday to Friday only
	ByGoal   bool // colour against the daily goal, not by hours
}

// weeksPerMonth sizes the heatmap for a number of months.
const weeksPerMonth = 4.35

func heatLevel(minutes int) int {
	level := 0
	for i, limit := range heatLevels {
		if minutes >= limit {
			level = i + 1
		}
	}
	return min(level, len(heatLevels)-1)
}

func heatmapView(cfg HeatConfig, data *sources.KimaiDataset, ctx ViewCtx) map[string]any {
	perDay := metrics.HoursByDay(data)
	today := todayOf(ctx)
	weeks := min(int(float64(cfg.Months)*weeksPerMonth+0.5), heatWeeks)
	rows := 7
	if cfg.Weekdays {
		rows = workDays
	}

	// Columns are weeks (Monday first), the last one holds today.
	lastMonday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	first := lastMonday.AddDate(0, 0, -7*(weeks-1))
	var cells []HeatCell
	total := 0
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		row := (int(d.Weekday()) + 6) % 7
		if row >= rows {
			continue
		}
		key := d.Format(isoDate)
		minutes := perDay[key]
		total += minutes
		cell := HeatCell{Level: heatLevel(minutes), Day: key, Hours: clockMinutes(minutes)}
		// The day's goal is its target in the Kimai work contract.
		goal := data.Contract.Minutes(d)
		if cfg.ByGoal && goal > 0 && minutes > 0 {
			cell.Goal = "under"
			if minutes >= goal {
				cell.Goal = "met"
			}
		}
		cells = append(cells, cell)
	}
	return map[string]any{"Cells": cells, "Rows": rows, "Total": total / minutesPerHour, "ByGoal": cfg.ByGoal && data.Contract != nil}
}

// ── cashflow ──

const (
	defaultCashDays = 90
	cashEventsShown = 6
)

type CashflowConfig struct {
	Days       int
	MinBalance float64 // a line and a warning below it; 0 = none
	Delay      int     // scenario: clients pay this many days later
}

// MoneyFlowConfig is the "money_flow" widget's config.
type MoneyFlowConfig struct {
	Paid bool // show what came in the last 30 days
}

// FlowStage is one segment of the money flow bar.
type FlowStage struct {
	Key    string // unbilled, drafts, sent, overdue
	Amount float64
	W      float64 // share of the bar in percent
	Link   string  // where to act on it
}

// moneyFlowView lays the stages out as one bar, from work to overdue.
func moneyFlowView(cfg MoneyFlowConfig, results map[string]any, ctx ViewCtx) map[string]any {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return map[string]any{}
	}
	kimai, _ := results[peerKimai].(*sources.KimaiDataset)
	f := metrics.MoneyFlowOf(kimai, ninja, todayOf(ctx))
	stages := []FlowStage{
		{Key: "unbilled", Amount: f.Unbilled, Link: "/billing"},
		{Key: "drafts", Amount: f.Drafts, Link: "/billing#drafts"},
		{Key: "sent", Amount: f.Sent, Link: ninja.URL},
		{Key: "overdue", Amount: f.Overdue, Link: "/hints?source=invoiceninja"},
	}
	if total := f.Total(); total > 0 {
		for i := range stages {
			stages[i].W = stages[i].Amount / total * pctFull
		}
	}
	view := map[string]any{"Stages": stages, "Total": f.Total(), "Currency": f.Currency}
	if cfg.Paid {
		view["Paid"] = f.Paid
	}
	return view
}

func cashflowView(cfg CashflowConfig, results map[string]any, ctx ViewCtx) map[string]any {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return map[string]any{}
	}
	in := cashInputsOf(cfg, ninja, results, ctx)
	points, events := metrics.Cashflow(in, todayOf(ctx), cfg.Days)
	if len(points) < 2 {
		return map[string]any{}
	}

	low, lowDay := metrics.CashLow(points)
	high := low
	for _, p := range points {
		high = max(high, p.Balance)
	}
	if cfg.MinBalance != 0 {
		low, high = min(low, cfg.MinBalance), max(high, cfg.MinBalance)
	}
	span := max(high-low, 1)
	step := float64(trendWidth) / float64(len(points)-1)
	coords := make([]string, len(points))
	for i, p := range points {
		coords[i] = formatPoint(float64(i)*step, trendHeight-(p.Balance-low)/span*(trendHeight-2*chartMargin)-chartMargin)
	}
	shown := events
	if len(shown) > cashEventsShown {
		shown = shown[:cashEventsShown]
	}
	out := map[string]any{"Path": "M" + joinPoints(coords), "W": trendWidth, "H": trendHeight, "Low": low, "LowDay": lowDay,
		"End": points[len(points)-1].Balance, "Relative": in.Sure == nil, "Events": shown, "Currency": ninja.Currency, "Delay": cfg.Delay}
	if cfg.MinBalance != 0 {
		out["MinY"] = fnum(trendHeight - (cfg.MinBalance-low)/span*(trendHeight-2*chartMargin) - chartMargin)
		out["Min"], out["BelowMin"] = cfg.MinBalance, lowDayBalance(points) < cfg.MinBalance
	}
	return out
}

// lowDayBalance is the lowest balance of a projection.
func lowDayBalance(points []metrics.CashPoint) float64 {
	low, _ := metrics.CashLow(points)
	return low
}

func init() {
	Tile[HeatConfig]{Key: "heatmap", Detail: dataDetail(heatmapDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceKimai, RefreshS: 3600,
		Fields: []Field{{Key: "months", Input: InputNumber, Default: 12, Min: "1", Max: "12"}, {Key: "weekdays", Input: InputCheck},
			{Key: "by_goal", Input: InputCheck}},
		Decode: func(r Raw) HeatConfig {
			return HeatConfig{Months: r.Int("months"), Weekdays: r.Bool("weekdays"), ByGoal: r.Bool("by_goal")}
		},
		Queries: ownData[HeatConfig], View: dataView(heatmapView)}.add()

	Tile[MoneyFlowConfig]{Key: "money_flow", Detail: moneyFlowDetail, Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceInvoiceNinja, RefreshS: 3600,
		Fields:  []Field{{Key: "show_paid", Input: InputCheck, Default: true}},
		Decode:  func(r Raw) MoneyFlowConfig { return MoneyFlowConfig{Paid: r.Bool("show_paid")} },
		Queries: func(MoneyFlowConfig) []Query { return append(dataQuery(nil), kimaiPeer) }, View: moneyFlowView}.add()

	Tile[CashflowConfig]{Key: "cashflow", Detail: cashflowDetail, Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceInvoiceNinja, RefreshS: 3600,
		Fields: []Field{{Key: "days", Input: InputNumber, Default: defaultCashDays, Min: "14", Max: "365"}, {Key: "min_balance", Input: InputNumber},
			{Key: "delay", Input: InputNumber, Min: "0", Max: "180"}},
		Decode: func(r Raw) CashflowConfig {
			return CashflowConfig{Days: r.Int("days"), MinBalance: r.Float("min_balance"), Delay: r.Int("delay")}
		},
		Queries:       func(CashflowConfig) []Query { return append(dataQuery(nil), bankPeers...) },
		DetailQueries: func(CashflowConfig) []Query { return []Query{peer(peerDepot, enums.ServiceGhostfolio)} }, View: cashflowView}.add()
}

// peerDepot names the space's Ghostfolio depot: no cash, so the forecast
// leaves it out; the dialog shows it as a line of its own.
const peerDepot = "ghostfolio"

// cashInputsOf gathers what the cashflow forecast reads.
func cashInputsOf(cfg CashflowConfig, ninja *sources.NinjaDataset, results map[string]any, ctx ViewCtx) metrics.CashInputs {
	in := metrics.CashInputs{Ninja: ninja, FixedMonthly: settingsFloat(settingsMap(ctx.Settings, "costs"), "fixed_monthly", 0),
		VATInterval: metrics.TaxVATInterval(ctx.Settings), VATMethod: metrics.TaxVATMethod(ctx.Settings), Center: metrics.CenterOf(ctx.Settings), DelayDays: cfg.Delay}
	in.Tax, in.HasTax = metrics.ParseTaxSettings(ctx.Settings)
	if sure, ok := bankOf(results); ok {
		in.Sure = sure
	}
	return in
}
