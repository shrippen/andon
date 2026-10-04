// Package widgets: insight widgets — metrics, tables, charts, budgets,
// deadlines, hints. All data comes from the "data" query of the widget's
// connection; view functions turn it into template values with the pure
// metrics package (no I/O here).
package widgets

import (
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	minutesPerHourInsight = 60
	defaultIncomeTaxRate  = 0.3
)

// Metric selects which KPI a "kpi" widget shows.
type Metric string

const (
	MetricHoursToday      Metric = "hours_today"
	MetricHoursWeek       Metric = "hours_week"
	MetricHoursMonth      Metric = "hours_month"
	MetricUtilization     Metric = "utilization"
	MetricUnbilled        Metric = "unbilled"
	MetricRevenueYTD      Metric = "revenue_ytd"
	MetricRevenueMonth    Metric = "revenue_month"
	MetricOpenAmount      Metric = "open_amount"
	MetricOverdueAmount   Metric = "overdue_amount"
	MetricVATLiability    Metric = "vat_liability"
	MetricTaxReserve      Metric = "tax_reserve"
	MetricAssetValue      Metric = "asset_value"
	MetricAssetsReady     Metric = "assets_ready"
	MetricRevenueForecast Metric = "revenue_forecast"
	MetricCash30          Metric = "cash_30"
	MetricEffectiveRate   Metric = "effective_rate"
	MetricLiquidity30     Metric = "liquidity_30"
	MetricNetWorth        Metric = "net_worth"
	MetricCash            Metric = "cash"
	MetricSafeToSpend     Metric = "safe_to_spend"
)

// moraleSlower: recent payments this many days slower than usual count as worse.
const moraleSlower = 10

// TableKind selects a "table" widget's row source.
type TableKind string

const (
	TableOpenInvoices TableKind = "open_invoices"
	TableUnbilled     TableKind = "unbilled"
	TableBudgets      TableKind = "budgets"
	TableClientShares TableKind = "client_shares"
	TableAssetDates   TableKind = "asset_dates"
	TableTrips        TableKind = "trips"
	TableRates        TableKind = "effective_rates"
	TableAppUsage     TableKind = "app_usage"
	TableMorale       TableKind = "payment_morale"
)

// ChartKind selects a "chart" widget's series.
type ChartKind string

const (
	ChartRevenue ChartKind = "revenue"
	ChartHours   ChartKind = "hours"
	ChartSeason  ChartKind = "seasonal"
)

// TrendMetric selects a "trend" widget's daily snapshot series.
type TrendMetric string

const (
	TrendRevenueYTD  TrendMetric = "revenue_ytd"
	TrendOpenAmount  TrendMetric = "open_amount"
	TrendMonthMinute TrendMetric = "month_min"
)

// KpiConfig is the "kpi" widget's config.
type KpiConfig struct {
	Metric  Metric
	Compare string  // prev_year (default), prev_month, off
	Target  float64 // 0 = none; colours the value
	Spark   bool    // the 12-month line (balance: 30 days)
	Free    bool    // balance: what is free to spend below the value
}

// KpiDetail is one line behind a KPI value, e.g. an open invoice.
type KpiDetail struct {
	Label  string
	Note   string // e.g. "12 days overdue", "14.5 h"
	Amount float64
}

// kpiDetailsShown caps the lines behind a KPI value.
const kpiDetailsShown = 12

// KPI comparisons.
const (
	comparePrevYear  = "prev_year"
	comparePrevMonth = "prev_month"
	compareOff       = "off"
)

// lowerBetter are metrics where staying under the target is good.
var lowerBetter = map[Metric]bool{MetricOpenAmount: true, MetricOverdueAmount: true, MetricVATLiability: true, MetricUnbilled: true}

func init() {
	Tile[KpiConfig]{Key: "kpi", Detail: kpiDetail, Category: CategoryInsight, Topic: TopicAnalysis, RefreshS: 600, DataChoice: true,
		Fields: []Field{sel("metric", string(MetricRevenueYTD), "hours_today", "hours_week", "hours_month", "utilization", "unbilled",
			"revenue_ytd", "revenue_month", "open_amount", "overdue_amount", "vat_liability", "tax_reserve",
			"asset_value", "assets_ready", "revenue_forecast", "cash_30", "liquidity_30", "effective_rate", "net_worth", "cash", "safe_to_spend"),
			sel("compare", comparePrevYear, comparePrevYear, comparePrevMonth, compareOff),
			{Key: "target_value", Input: InputNumber, Min: "0"}, {Key: "spark", Input: InputCheck, Default: true}, {Key: "free", Input: InputCheck}},
		Decode: func(r Raw) KpiConfig {
			return KpiConfig{Metric: Metric(r.Pick("metric")), Compare: r.Pick("compare"), Target: r.Float("target_value"),
				Spark: r.Bool("spark"), Free: r.Bool("free")}
		},
		Queries: kpiQueries, View: kpiView}.add()
}

// monthDelta is this month against the last for metrics with months.
func monthDelta(metric Metric, data any, today time.Time) (float64, bool) {
	var prev, cur float64
	switch d := data.(type) {
	case *sources.NinjaDataset:
		if metric != MetricRevenueMonth {
			return 0, false
		}
		months := metrics.NinjaByMonth(d, today, 2)
		if len(months) < 2 {
			return 0, false
		}
		prev, cur = months[0].Net, months[1].Net
	case *sources.KimaiDataset:
		if metric != MetricHoursMonth {
			return 0, false
		}
		hours, _ := kimaiMonthHours(d, today, 2)
		prev, cur = hours[0], hours[1]
	default:
		return 0, false
	}
	if prev == 0 {
		return 0, false
	}
	return (cur - prev) / prev, true
}

// shapeKpi applies the tile's options to a computed KPI.
func shapeKpi(kpi *KpiResult, cfg KpiConfig, data any, today time.Time) {
	kpi.DeltaKey = "kpi.vs_last_year"
	switch cfg.Compare {
	case compareOff:
		kpi.HasDelta = false
	case comparePrevMonth:
		kpi.Delta, kpi.HasDelta = monthDelta(cfg.Metric, data, today)
		kpi.DeltaKey = "kpi.vs_last_month"
	}
	if cfg.Target > 0 {
		good := kpi.Value >= cfg.Target
		if lowerBetter[cfg.Metric] {
			good = kpi.Value <= cfg.Target
		}
		kpi.Target = "bad"
		if good {
			kpi.Target = "good"
		}
	}
	if cfg.Spark {
		kpi.Spark = kpiSpark(cfg.Metric, data, today)
	}
	if kpi.Spark != nil && cfg.Metric == MetricCash {
		kpi.SparkDays = cashDays
	}
}

// TableConfig is the "table" widget's config.
type TableConfig struct {
	Table    TableKind
	Limit    int
	HideCols []string // column names (as shown, any language) or keys, lower case
	Sort     string   // "" (as delivered), amount_desc, amount_asc, name, date
	SumRow   bool
}

func init() {
	Tile[TableConfig]{Key: "table", Width: WidthFull, Detail: tableDetail, Category: CategoryInsight, Topic: TopicAnalysis, RefreshS: 600, DataChoice: true,
		Fields: []Field{sel("table", "open_invoices", "open_invoices", "unbilled", "budgets", "client_shares", "asset_dates", "trips", "effective_rates", "app_usage", "payment_morale",
			"full_rates", "unbilled_aging", "payment_matches", "missing_receipts", "subscriptions", "budget_forecast", "project_margins", "exposure", "domain_chain"),
			{Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"}, {Key: "hide_cols", Input: InputList},
			sel("sort", sortAsIs, sortAsIs, sortAmountDesc, sortAmountAsc, sortName, sortDate), {Key: "sum_row", Input: InputCheck}},
		Decode: decodeTable, Queries: tableQueries, View: tableView}.add()
}

func decodeTable(r Raw) TableConfig {
	var hide []string
	for _, c := range r.List("hide_cols") {
		hide = append(hide, strings.ToLower(strings.TrimSpace(c)))
	}
	return TableConfig{Table: TableKind(r.Pick("table")), Limit: r.Int("limit"), HideCols: hide, Sort: r.Pick("sort"),
		SumRow: r.Bool("sum_row")}
}

// ChartConfig is the "chart" widget's config.
type ChartConfig struct {
	Chart    ChartKind
	Months   int
	ShowPrev bool // last year (or the seasonal average) as an outline
	Values   bool // the value over each bar
	GoalLine bool // revenue: the year's goal per month
}

func init() {
	Tile[ChartConfig]{Key: "chart", Width: WidthFull, Detail: chartDetail, Category: CategoryInsight, Topic: TopicAnalysis, RefreshS: 3600, DataChoice: true,
		Fields: []Field{sel("chart", "revenue", "revenue", "hours", "seasonal"), {Key: "months", Input: InputNumber, Default: 12, Min: "3", Max: "24"},
			{Key: "show_prev", Input: InputCheck, Default: true}, {Key: "values", Input: InputCheck}, {Key: "goal_line", Input: InputCheck}},
		Decode: func(r Raw) ChartConfig {
			return ChartConfig{Chart: ChartKind(r.Pick("chart")), Months: r.Int("months"), ShowPrev: r.Bool("show_prev"),
				Values: r.Bool("values"), GoalLine: r.Bool("goal_line")}
		},
		Queries: ownData[ChartConfig], View: chartView}.add()
}

// boolOr reads a checkbox that defaults to on.
func boolOr(v any, def bool) bool {
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

// ProgressConfig is the "progress" widget's config.
type ProgressConfig struct {
	Goal     bool
	Projects []string // only these budgets (lower case); empty = all
	Soll     bool     // the "where it should be today" mark
	Warn     float64  // yellow from this many points ahead (0..1)
}

func init() {
	Tile[ProgressConfig]{Key: "progress", Width: WidthFull, Detail: progressDetail, Category: CategoryInsight, Topic: TopicAnalysis, RefreshS: 600, DataChoice: true,
		Fields: []Field{{Key: "goal", Input: InputCheck, Default: true}, {Key: "projects", Input: InputList}, {Key: "soll", Input: InputCheck, Default: true},
			{Key: "warn_ahead", Input: InputNumber, Default: 10, Min: "1", Max: "100"}},
		Decode: decodeProgress, Queries: ownData[ProgressConfig], View: progressView}.add()
}

func decodeProgress(r Raw) ProgressConfig {
	// Out of range means the default, not the nearest bound: 0 → 10 %.
	warn := progressSlack
	if v := asFloat(r.Get("warn_ahead")); v > 0 && v <= pctFull {
		warn = v / pctFull
	}
	return ProgressConfig{Goal: r.Bool("goal"), Projects: r.Lower("projects"), Soll: r.Bool("soll"), Warn: warn}
}

// HintsConfig is the "hints" widget's config.
type HintsConfig struct {
	Sources     []string
	MinSeverity int
	Limit       int
	Topic       rules.Topic // updates/backups widgets
	Sort        string      // "" = most urgent first, "value" = largest amount, "age" = oldest
	Buttons     bool        // done and later on each line
	NoLevels    bool        // hide the level bar
	DueDays     int         // > 0: only hints due within that many days, soonest first
}

// Hint list orders besides the default (most urgent first).
const (
	hintSortUrgency = "urgency" // the select's name for "" (most urgent first)
	HintSortValue   = "value"
	HintSortAge     = "age"
)

// hintSort is the sort select as HintsConfig.Sort: urgency is "".
func hintSort(r Raw) string {
	if s := r.Pick("sort"); s != hintSortUrgency {
		return s
	}
	return ""
}

// Hint lists (ExtraHints) are calm without hints.
func hintsCalm(v map[string]any) bool { return v["Hints"] != nil && lenOf(v["Hints"]) == 0 }

func init() {
	// updates: a hints list limited to the update rules.
	Tile[HintsConfig]{Key: "updates", Detail: updatesDetail, DetailQueries: releaseQuery, Template: "widgets/topic", Category: CategoryInsight, Topic: TopicHomelab, RefreshS: 600, Extra: ExtraHints,
		Fields: []Field{{Key: "limit", Input: InputNumber, Default: 20, Min: "1", Max: "50"}, {Key: "sources", Input: InputList}, sel("sort", "urgency", "urgency", "age")},
		Calm:   hintsCalm,
		Decode: func(r Raw) HintsConfig {
			return HintsConfig{MinSeverity: int(enums.SeverityInfo), Limit: r.Int("limit"), Topic: rules.TopicUpdates,
				Sources: r.Lower("sources"), Sort: hintSort(r)}
		}}.add()

	// expiries: every hint with a due date (certificates, domains,
	// warranties, contracts, renewals, tax) on one timeline.
	Tile[HintsConfig]{Key: "expiries", Detail: expiriesDetail, Category: CategoryInsight, Topic: TopicOverview, RefreshS: 3600, Extra: ExtraHints,
		Fields: []Field{{Key: "days", Input: InputNumber, Default: 90, Min: "7", Max: "400"}, {Key: "limit", Input: InputNumber, Default: 15, Min: "1", Max: "50"}, {Key: "sources", Input: InputList}},
		Decode: func(r Raw) HintsConfig {
			return HintsConfig{MinSeverity: int(enums.SeverityInfo), Limit: r.Int("limit"),
				Sources: r.Lower("sources"), DueDays: r.Int("days"), NoLevels: true}
		}}.add()
}

// severityChoices are the hint levels a tile can start from, as the
// select sends them: "10" info, "20" warning, "30" critical.
var severityChoices = []string{strconv.Itoa(int(enums.SeverityInfo)), strconv.Itoa(int(enums.SeverityWarn)),
	strconv.Itoa(int(enums.SeverityCritical))}

func init() {
	Tile[HintsConfig]{Key: "hints", Width: WidthFull, Detail: hintsDetail, Category: CategoryInsight, Topic: TopicOverview, RefreshS: 300, Extra: ExtraHints,
		Fields: []Field{{Key: "sources", Input: InputList}, sel("min_severity", severityChoices[0], severityChoices...), {Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"},
			{Key: "show_buttons", Input: InputCheck}, sel("sort", hintSortUrgency, hintSortUrgency, HintSortValue, HintSortAge),
			{Key: "show_levels", Input: InputCheck, Default: true}},
		Renames: []rename{
			{from: "by_value", to: "sort", value: func(v any) (any, bool) { return HintSortValue, asBool(v) }},
			{from: "levels", to: "show_levels"},
			{from: "buttons", to: "show_buttons"},
		},
		Calm: hintsCalm, Decode: decodeHints}.add()
}

func decodeHints(r Raw) HintsConfig {
	// Stored as the select's text ("20") or, older, as a number; any
	// level in between counts too, so this is no Pick.
	level := r.Get("min_severity")
	if s, ok := level.(string); ok {
		n, _ := strconv.Atoi(s)
		level = float64(n)
	}
	minSeverity := clampInt(asInt(level, int(enums.SeverityInfo)), int(enums.SeverityInfo), int(enums.SeverityCritical))
	return HintsConfig{Sources: r.Lower("sources"), MinSeverity: minSeverity, Limit: r.Int("limit"),
		Buttons: r.Bool("show_buttons"), NoLevels: !r.Bool("show_levels"), Sort: hintSort(r)}
}

// TrendConfig is the "trend" widget's config.
type TrendConfig struct {
	Metric TrendMetric
	Days   int
	Target float64 // 0 = no target line
	Smooth bool    // 7-day centre (mean or median, space setting)
}

// smoothDays is the window of a smoothed trend.
const smoothDays = 7

// MaxTrendDays is the longest span a trend shows; older daily points go.
const MaxTrendDays = 730

func init() {
	Tile[TrendConfig]{Key: "trend", Detail: trendDetail, Category: CategoryInsight, Topic: TopicAnalysis, RefreshS: 3600, Extra: ExtraPoints,
		Fields: []Field{sel("metric", string(TrendOpenAmount), "revenue_ytd", "open_amount", "month_min"), {Key: "days", Input: InputNumber, Default: 90, Min: "7", Max: strconv.Itoa(MaxTrendDays)},
			{Key: "target_value", Input: InputNumber, Min: "0"}, {Key: "smooth", Input: InputCheck}},
		Decode: func(r Raw) TrendConfig {
			return TrendConfig{Metric: TrendMetric(r.Pick("metric")), Days: r.Int("days"), Target: r.Float("target_value"),
				Smooth: r.Bool("smooth")}
		},
		View: trendView,
		DetailQueries: func(cfg TrendConfig) []Query {
			if cfg.Metric == TrendMonthMinute {
				return nil
			}
			return []Query{peer(peerNinja, enums.ServiceInvoiceNinja)}
		}}.add()
}

// DeadlinesConfig is the "deadlines" widget's config.
type DeadlinesConfig struct {
	Days                     int
	VAT, Prepayments, Annual bool // which kinds to list
	Amounts                  bool
}

func init() {
	Tile[DeadlinesConfig]{Key: "deadlines", Width: WidthFull, Detail: deadlinesDetail, Category: CategoryInsight, Topic: TopicOverview, RefreshS: 3600,
		Fields: []Field{{Key: "days", Input: InputNumber, Default: 45, Min: "7", Max: "400"}, {Key: "show_vat", Input: InputCheck, Default: true},
			{Key: "show_prepayment", Input: InputCheck, Default: true}, {Key: "show_annual", Input: InputCheck, Default: true},
			{Key: "amounts", Input: InputCheck, Default: true}},
		Decode: func(r Raw) DeadlinesConfig {
			return DeadlinesConfig{Days: r.Int("days"), VAT: r.Bool("show_vat"), Prepayments: r.Bool("show_prepayment"),
				Annual: r.Bool("show_annual"), Amounts: r.Bool("amounts")}
		},
		View: deadlinesView,
		Calm: func(v map[string]any) bool { return v["Configured"] == true && lenOf(v["Items"]) == 0 }}.add()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func settingsMap(settings map[string]any, key string) map[string]any {
	m, _ := settings[key].(map[string]any)
	return m
}

func settingsFloat(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		if f := asFloat(v); f != 0 {
			return f
		}
	}
	return def
}

// ── KPI ──

// KpiResult is one "kpi" widget's computed value plus an optional
// translated sub-caption (see the widgets/kpi.html template).
type KpiResult struct {
	Kind      string // "money", "percent", "hours", "count"
	Value     float64
	Currency  string
	HasDelta  bool
	Delta     float64
	SubKey    string // "" if none
	SubHours  float64
	SubCount  int
	SubIn     float64 // liquidity: expected income
	SubOut    float64 // liquidity: fixed costs
	SubStart  string
	SubEnd    string
	SubGoal   float64
	SubRate   int
	Spark     *Spark      // last 12 months, where the metric has a history
	SparkDays int         // the line covers days instead of months (balance)
	DeltaKey  string      // what Delta compares with
	Details   []KpiDetail // what the value is made of, opened on click

	Target string // "good", "bad" or "" (no target)
}

// sparkMonths is how far back a KPI's line reaches; a balance, which
// changes daily, shows cashDays days.
const (
	sparkMonths = 12
	cashDays    = 30
)

// kimaiMonthHours is the tracked hours of each of the last n months,
// oldest first; prev is the same month a year before.
func kimaiMonthHours(data *sources.KimaiDataset, today time.Time, n int) (cur, prev []float64) {
	for back := n - 1; back >= 0; back-- {
		start := metrics.AddMonths(today, -back)
		end := metrics.AddMonths(start, 1).AddDate(0, 0, -1)
		prevStart := time.Date(start.Year()-1, start.Month(), 1, 0, 0, 0, 0, time.UTC)
		prevEnd := metrics.AddMonths(prevStart, 1).AddDate(0, 0, -1)
		cur = append(cur, float64(metrics.KimaiMinutesBetween(data, start, end, metrics.HoursAll))/minutesPerHourInsight)
		prev = append(prev, float64(metrics.KimaiMinutesBetween(data, prevStart, prevEnd, metrics.HoursAll))/minutesPerHourInsight)
	}
	return cur, prev
}

// kimaiMonthHoursOf is the hours of the n months up to today's, of one
// kind.
func kimaiMonthHoursOf(data *sources.KimaiDataset, today time.Time, n int, kind metrics.Hours) []float64 {
	var out []float64
	for back := n - 1; back >= 0; back-- {
		start := metrics.AddMonths(today, -back)
		end := metrics.AddMonths(start, 1).AddDate(0, 0, -1)
		out = append(out, float64(metrics.KimaiMinutesBetween(data, start, end, kind))/minutesPerHourInsight)
	}
	return out
}

// kpiSpark is the monthly line behind a metric, nil when it has none. It
// ends with last month: the running month would always dip.
func kpiSpark(metric Metric, data any, today time.Time) *Spark {
	return SparkOf(kpiSeries(metric, data, today))
}

// kpiSeries is the history behind a KPI's line: months, or days for a
// balance; nil where the metric has none.
func kpiSeries(metric Metric, data any, today time.Time) []float64 {
	lastMonth := metrics.AddMonths(today, -1)
	switch d := data.(type) {
	case *sources.NinjaDataset:
		if metric != MetricRevenueYTD && metric != MetricRevenueMonth {
			return nil
		}
		var values []float64
		for _, m := range metrics.NinjaByMonth(d, lastMonth, sparkMonths) {
			values = append(values, m.Net)
		}
		return values
	case *sources.KimaiDataset:
		if metric != MetricHoursMonth && metric != MetricHoursWeek && metric != MetricUtilization {
			return nil
		}
		cur, _ := kimaiMonthHours(d, lastMonth, sparkMonths)
		return cur
	case *sources.SureDataset:
		if metric != MetricCash {
			return nil
		}
		return metrics.SureCashDays(d, today, cashDays)
	}
	return nil
}

func kpiKimai(metric Metric, data *sources.KimaiDataset, ctx ViewCtx) *KpiResult {
	stats := metrics.KimaiSummaryOf(data, todayOf(ctx))
	switch metric {
	case MetricHoursToday:
		return &KpiResult{Kind: "hours", Value: float64(stats.TodayMin) / minutesPerHourInsight}
	case MetricHoursWeek:
		return &KpiResult{Kind: "hours", Value: float64(stats.WeekMin) / minutesPerHourInsight}
	case MetricHoursMonth:
		return &KpiResult{Kind: "hours", Value: float64(stats.MonthMin) / minutesPerHourInsight}
	case MetricUtilization:
		if stats.Utilization == nil {
			return nil
		}
		return &KpiResult{Kind: "percent", Value: *stats.Utilization, SubKey: "kpi.of_target",
			SubHours: float64(stats.TargetMonthMin) / minutesPerHourInsight}
	case MetricUnbilled:
		var amount float64
		var minutes int
		for _, g := range stats.Unbilled {
			amount += g.Amount
			minutes += g.Minutes
		}
		result := &KpiResult{Kind: "money", Value: amount, SubKey: "kpi.hours", SubHours: float64(minutes) / minutesPerHourInsight}
		for _, g := range stats.Unbilled[:min(len(stats.Unbilled), kpiDetailsShown)] {
			result.Details = append(result.Details, KpiDetail{Label: g.Customer, Amount: g.Amount,
				Note: strconv.FormatFloat(float64(g.Minutes)/minutesPerHourInsight, 'f', 1, 64) + " h"})
		}
		return result
	}
	return nil
}

// invoiceDetails lists open invoices behind a KPI value.
func invoiceDetails(open []metrics.NinjaOpenInvoice) []KpiDetail {
	var out []KpiDetail
	for _, i := range open[:min(len(open), kpiDetailsShown)] {
		d := KpiDetail{Label: i.Number + " · " + i.Client, Amount: i.Balance}
		if i.OverdueDays > 0 {
			d.Note = "+" + strconv.Itoa(i.OverdueDays) + " d"
		}
		out = append(out, d)
	}
	return out
}

func kpiNinja(metric Metric, data *sources.NinjaDataset, peers map[string]any, ctx ViewCtx) *KpiResult {
	tax := settingsMap(ctx.Settings, "tax")
	interval := metrics.TaxVATInterval(ctx.Settings)
	method := metrics.TaxVATMethod(ctx.Settings)
	today := todayOf(ctx)
	stats := metrics.NinjaSummaryOf(data, today, interval, method)

	switch metric {
	case MetricRevenueYTD:
		if stats.RevenuePrevYTD == 0 {
			return &KpiResult{Kind: "money", Value: stats.RevenueYTD, Currency: stats.Currency}
		}
		delta := (stats.RevenueYTD - stats.RevenuePrevYTD) / stats.RevenuePrevYTD
		return &KpiResult{Kind: "money", Value: stats.RevenueYTD, Currency: stats.Currency, HasDelta: true, Delta: delta}
	case MetricRevenueMonth:
		return &KpiResult{Kind: "money", Value: stats.RevenueMonth, Currency: stats.Currency}
	case MetricOpenAmount:
		return &KpiResult{Kind: "money", Value: stats.OpenAmount, Currency: stats.Currency,
			SubKey: "kpi.invoices", SubCount: len(stats.Open), Details: invoiceDetails(stats.Open)}
	case MetricOverdueAmount:
		var amount float64
		for _, i := range stats.Overdue {
			amount += i.Balance
		}
		return &KpiResult{Kind: "money", Value: amount, Currency: stats.Currency,
			SubKey: "kpi.invoices", SubCount: len(stats.Overdue), Details: invoiceDetails(stats.Overdue)}
	case MetricVATLiability:
		return &KpiResult{Kind: "money", Value: stats.VAT.Liability, Currency: stats.Currency,
			SubKey: "kpi.vat_period", SubStart: stats.VAT.Start, SubEnd: stats.VAT.End}
	case MetricRevenueForecast:
		forecast := metrics.NinjaForecastYear(data, today)
		goal := settingsFloat(settingsMap(ctx.Settings, "goals"), "revenue_year", 0)
		result := &KpiResult{Kind: "money", Value: forecast, Currency: stats.Currency}
		if goal > 0 {
			result.SubKey, result.SubGoal = "kpi.of_goal", goal
		}
		return result
	case MetricCash30:
		return &KpiResult{Kind: "money", Value: metrics.NinjaCashExpected(data, today, 30), Currency: stats.Currency,
			SubKey: "kpi.cash_30"}
	case MetricLiquidity30:
		// 30 days ≈ one month of fixed costs; Sure's recurring payments
		// replace the manual figure when a Sure connection exists.
		income := metrics.NinjaCashExpected(data, today, 30)
		fixed := settingsFloat(settingsMap(ctx.Settings, "costs"), "fixed_monthly", 0)
		if sure, ok := peers[peerSure].(*sources.SureDataset); ok {
			fixed = metrics.SureDue(sure, today, 30)
		}
		return &KpiResult{Kind: "money", Value: income - fixed, Currency: stats.Currency,
			SubKey: "kpi.liquidity", SubIn: income, SubOut: fixed}
	case MetricEffectiveRate:
		kimai, ok := peers[peerKimai].(*sources.KimaiDataset)
		if !ok {
			return nil
		}
		rows, overall := metrics.EffectiveRates(kimai, data, today)
		return &KpiResult{Kind: "money", Value: overall, Currency: stats.Currency, SubKey: "kpi.per_hour", SubCount: len(rows)}
	case MetricSafeToSpend:
		sure, ok := peers[peerSure].(*sources.SureDataset)
		if !ok {
			return nil
		}
		s := metrics.SafeToSpend(sure, data, today, interval, method, settingsFloat(tax, "income_tax_rate", defaultIncomeTaxRate))
		return &KpiResult{Kind: "money", Value: s.Free, Currency: stats.Currency, SubKey: "kpi.safe", SubIn: s.Cash, SubOut: s.VAT + s.IncomeTax + s.Fixed}
	case MetricTaxReserve:
		rate := settingsFloat(tax, "income_tax_rate", defaultIncomeTaxRate)
		var expenses float64
		for _, e := range data.Expenses {
			if d, ok := metrics.ParseDay(e.Date); ok && d.Year() == today.Year() {
				expenses += e.Amount - e.Tax
			}
		}
		surplus := stats.RevenueYTD - expenses
		if surplus < 0 {
			surplus = 0
		}
		return &KpiResult{Kind: "money", Value: stats.VAT.Liability + surplus*rate, Currency: stats.Currency,
			SubKey: "kpi.reserve", SubRate: int(rate*100 + 0.5)}
	}
	return nil
}

func kpiSure(cfg KpiConfig, data *sources.SureDataset, peers map[string]any, ctx ViewCtx) *KpiResult {
	switch cfg.Metric {
	case MetricNetWorth:
		return &KpiResult{Kind: "money", Value: data.NetWorth, Currency: data.Currency}
	case MetricCash:
		kpi := &KpiResult{Kind: "money", Value: metrics.SureCash(data), Currency: data.Currency}
		ninja, ok := peers[peerNinja].(*sources.NinjaDataset)
		if !cfg.Free || !ok {
			return kpi
		}

		// Free to spend: the balance less VAT, income tax and 30 days of
		// fixed costs (see MetricSafeToSpend).
		rate := settingsFloat(settingsMap(ctx.Settings, "tax"), "income_tax_rate", defaultIncomeTaxRate)
		s := metrics.SafeToSpend(data, ninja, todayOf(ctx), metrics.TaxVATInterval(ctx.Settings), metrics.TaxVATMethod(ctx.Settings), rate)
		kpi.SubKey, kpi.SubIn = "kpi.free", s.Free
		return kpi
	}
	return nil
}

func kpiSnipe(metric Metric, data *sources.SnipeDataset, ctx ViewCtx) *KpiResult {
	stats := metrics.SnipeSummaryOf(data, todayOf(ctx))
	switch metric {
	case MetricAssetValue:
		return &KpiResult{Kind: "money", Value: stats.Value, SubKey: "kpi.assets", SubCount: stats.Assets}
	case MetricAssetsReady:
		return &KpiResult{Kind: "count", Value: float64(stats.Ready)}
	}
	return nil
}

func kpiView(cfg KpiConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["data"]
	if !ok || data == nil {
		return map[string]any{}
	}
	var kpi *KpiResult
	switch enums.ServiceType(ctx.Service) {
	case enums.ServiceKimai:
		kpi = kpiKimai(cfg.Metric, data.(*sources.KimaiDataset), ctx)
	case enums.ServiceInvoiceNinja:
		kpi = kpiNinja(cfg.Metric, data.(*sources.NinjaDataset), results, ctx)
	case enums.ServiceSure:
		kpi = kpiSure(cfg, data.(*sources.SureDataset), results, ctx)
	case enums.ServiceSnipeIT:
		kpi = kpiSnipe(cfg.Metric, data.(*sources.SnipeDataset), ctx)
	}
	if kpi != nil {
		shapeKpi(kpi, cfg, data, todayOf(ctx))
	}
	return map[string]any{"KPI": kpi, "Unsupported": kpi == nil}
}
