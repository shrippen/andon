// Package widgets: insight widgets — metrics, tables, charts, budgets,
// deadlines, hints. All data comes from the "data" query of the widget's
// connection; view functions turn it into template values with the pure
// metrics package (no I/O here).
package widgets

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
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
	Spark   bool    // the 12-month line
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

func decodeKpi(raw map[string]any) any {
	metric := Metric(asString(raw["metric"]))
	if metric == "" {
		metric = MetricRevenueYTD
	}
	compare := asString(raw["compare"])
	if compare != comparePrevMonth && compare != compareOff {
		compare = comparePrevYear
	}
	spark, set := raw["spark"].(bool)
	return KpiConfig{Metric: metric, Compare: compare, Target: max(asFloat(raw["target_value"]), 0), Spark: spark || !set}
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
}

// TableConfig is the "table" widget's config.
type TableConfig struct {
	Table    TableKind
	Limit    int
	HideCols []string // column names (as shown, any language) or keys, lower case
	Sort     string   // "" (as delivered), amount_desc, amount_asc, name, date
	SumRow   bool
}

func decodeTable(raw map[string]any) any {
	table := TableKind(asString(raw["table"]))
	if table == "" {
		table = TableOpenInvoices
	}
	var hide []string
	for _, c := range asStringList(raw["hide_cols"]) {
		hide = append(hide, strings.ToLower(strings.TrimSpace(c)))
	}
	return TableConfig{Table: table, Limit: clampInt(asInt(raw["limit"], 8), 1, 50), HideCols: hide, Sort: asString(raw["sort"]),
		SumRow: asBool(raw["sum_row"])}
}

// ChartConfig is the "chart" widget's config.
type ChartConfig struct {
	Chart    ChartKind
	Months   int
	ShowPrev bool // last year (or the seasonal average) as an outline
	Values   bool // the value over each bar
	GoalLine bool // revenue: the year's goal per month
}

func decodeChart(raw map[string]any) any {
	chart := ChartKind(asString(raw["chart"]))
	if chart == "" {
		chart = ChartRevenue
	}
	return ChartConfig{Chart: chart, Months: clampInt(asInt(raw["months"], 12), 3, 24), ShowPrev: boolOr(raw["show_prev"], true),
		Values: asBool(raw["values"]), GoalLine: asBool(raw["goal_line"])}
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

func decodeProgress(raw map[string]any) any {
	goal := true
	if v, ok := raw["goal"]; ok {
		goal = asBool(v)
	}
	var projects []string
	for _, p := range asStringList(raw["projects"]) {
		projects = append(projects, strings.ToLower(p))
	}
	warn := progressSlack
	if v := asFloat(raw["warn_ahead"]); v > 0 {
		warn = v / pctFull
	}
	return ProgressConfig{Goal: goal, Projects: projects, Soll: boolOr(raw["soll"], true), Warn: warn}
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
	HintSortValue = "value"
	HintSortAge   = "age"
)

// decodeTopic builds the decoder of a topic widget: a hints list limited
// to one topic's rules.
func decodeTopic(topic rules.Topic) DecodeFunc {
	return func(raw map[string]any) any {
		sort, _ := raw["sort"].(string)
		if sort != HintSortAge {
			sort = ""
		}
		return HintsConfig{MinSeverity: int(enums.SeverityInfo), Limit: clampInt(asInt(raw["limit"], 20), 1, 50), Topic: topic,
			Sources: asStringList(raw["sources"]), Sort: sort}
	}
}

// decodeExpiries: every hint with a due date (certificates, domains,
// warranties, contracts, renewals, tax) on one timeline.
func decodeExpiries(raw map[string]any) any {
	return HintsConfig{MinSeverity: int(enums.SeverityInfo), Limit: clampInt(asInt(raw["limit"], 15), 1, 50),
		Sources: asStringList(raw["sources"]), DueDays: clampInt(asInt(raw["days"], 90), 7, 400), NoLevels: true}
}

func decodeHints(raw map[string]any) any {
	minSeverity := clampInt(asInt(raw["min_severity"], int(enums.SeverityInfo)), int(enums.SeverityInfo), int(enums.SeverityCritical))
	cfg := HintsConfig{Sources: asStringList(raw["sources"]), MinSeverity: minSeverity, Limit: clampInt(asInt(raw["limit"], 8), 1, 50),
		Buttons: asBool(raw["buttons"]), NoLevels: !boolOr(raw["levels"], true)}
	if asBool(raw["by_value"]) {
		cfg.Sort = HintSortValue
	}
	return cfg
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

func decodeTrend(raw map[string]any) any {
	metric := TrendMetric(asString(raw["metric"]))
	if metric == "" {
		metric = TrendOpenAmount
	}
	return TrendConfig{Metric: metric, Days: clampInt(asInt(raw["days"], 90), 7, MaxTrendDays), Target: max(asFloat(raw["target_value"]), 0),
		Smooth: asBool(raw["smooth"])}
}

// DeadlinesConfig is the "deadlines" widget's config.
type DeadlinesConfig struct {
	Days                     int
	VAT, Prepayments, Annual bool // which kinds to list
	Amounts                  bool
}

func decodeDeadlines(raw map[string]any) any {
	return DeadlinesConfig{Days: clampInt(asInt(raw["days"], 45), 7, 400), VAT: boolOr(raw["show_vat"], true),
		Prepayments: boolOr(raw["show_prepayment"], true), Annual: boolOr(raw["show_annual"], true), Amounts: boolOr(raw["amounts"], true)}
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

func parseToday(iso string) time.Time {
	if t, ok := metrics.ParseDay(iso); ok {
		return t
	}
	return time.Now().UTC()
}

// ── KPI ──

// KpiResult is one "kpi" widget's computed value plus an optional
// translated sub-caption (see the widgets/kpi.html template).
type KpiResult struct {
	Kind     string // "money", "percent", "hours", "count"
	Value    float64
	Currency string
	HasDelta bool
	Delta    float64
	SubKey   string // "" if none
	SubHours float64
	SubCount int
	SubIn    float64 // liquidity: expected income
	SubOut   float64 // liquidity: fixed costs
	SubStart string
	SubEnd   string
	SubGoal  float64
	SubRate  int
	Spark    *Spark      // last 12 months, where the metric has a history
	DeltaKey string      // what Delta compares with
	Details  []KpiDetail // what the value is made of, opened on click

	Target string // "good", "bad" or "" (no target)
}

// sparkMonths is how far back a KPI's line reaches.
const sparkMonths = 12

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
		return SparkOf(values)
	case *sources.KimaiDataset:
		if metric != MetricHoursMonth && metric != MetricHoursWeek && metric != MetricUtilization {
			return nil
		}
		cur, _ := kimaiMonthHours(d, lastMonth, sparkMonths)
		return SparkOf(cur)
	}
	return nil
}

func kpiKimai(metric Metric, data *sources.KimaiDataset, ctx ViewCtx) *KpiResult {
	hoursPerDay := settingsFloat(settingsMap(ctx.Settings, "goals"), "hours_per_day", 0)
	stats := metrics.KimaiSummaryOf(data, parseToday(ctx.Today), hoursPerDay)
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
	today := parseToday(ctx.Today)
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

func kpiSure(metric Metric, data *sources.SureDataset) *KpiResult {
	switch metric {
	case MetricNetWorth:
		return &KpiResult{Kind: "money", Value: data.NetWorth, Currency: data.Currency}
	case MetricCash:
		return &KpiResult{Kind: "money", Value: metrics.SureCash(data), Currency: data.Currency}
	}
	return nil
}

func kpiSnipe(metric Metric, data *sources.SnipeDataset, ctx ViewCtx) *KpiResult {
	stats := metrics.SnipeSummaryOf(data, parseToday(ctx.Today))
	switch metric {
	case MetricAssetValue:
		return &KpiResult{Kind: "money", Value: stats.Value, SubKey: "kpi.assets", SubCount: stats.Assets}
	case MetricAssetsReady:
		return &KpiResult{Kind: "count", Value: float64(stats.Ready)}
	}
	return nil
}

func kpiView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(KpiConfig)
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
		kpi = kpiSure(cfg.Metric, data.(*sources.SureDataset))
	case enums.ServiceSnipeIT:
		kpi = kpiSnipe(cfg.Metric, data.(*sources.SnipeDataset), ctx)
	}
	if kpi != nil {
		shapeKpi(kpi, cfg, data, parseToday(ctx.Today))
	}
	return map[string]any{"KPI": kpi, "Unsupported": kpi == nil}
}

// ── Tables ──

// Col is one table column: its header key and how to format each row's
// value at the same index.
type Col struct{ Label, Format string }

// numericFormats are the column formats set right-aligned.
var numericFormats = map[string]bool{"money": true, "hours": true, "km": true, "daycount": true, "late": true}

// Numeric tells whether the column holds numbers (right-aligned).
func (c Col) Numeric() bool { return numericFormats[c.Format] }

// Row is one table row; Values line up with the widget's Cols.
type Row struct{ Values []any }

func colsFor(kind TableKind) []Col {
	switch kind {
	case TableOpenInvoices:
		return []Col{{"number", "text"}, {"client", "text"}, {"due", "day"}, {"late", "late"}, {"amount", "money"}}
	case TableClientShares:
		return []Col{{"client", "text"}, {"amount", "money"}, {"share", "pct"}}
	case TableUnbilled:
		return []Col{{"customer", "text"}, {"hours", "hours"}, {"amount", "money"}, {"oldest", "day"}}
	case TableBudgets:
		return []Col{{"project", "text"}, {"used", "bar"}}
	case TableAssetDates:
		return []Col{{"name", "text"}, {"kind", "upcoming"}, {"due", "day"}}
	case TableTrips:
		return []Col{{"area", "text"}, {"day", "day"}, {"km", "km"}, {"away", "hours"}}
	case TableRates:
		return []Col{{"customer", "text"}, {"hours", "hours"}, {"amount", "money"}, {"rate", "money"}}
	case TableAppUsage:
		return []Col{{"app", "text"}, {"logins", "text"}, {"users", "text"}}
	case TableMorale:
		return []Col{{"client", "text"}, {"avg_days", "daycount"}, {"recent_days", "daycount"}, {"invoices", "text"}}
	}
	if cols := crossCols(kind); cols != nil {
		return cols
	}
	return homelabCols(kind)
}

func tableRows(kind TableKind, results map[string]any, ctx ViewCtx) ([]Row, bool) {
	data, ok := results["data"]
	if !ok || data == nil {
		return nil, false
	}
	if rows, ok := crossRows(kind, data, results, ctx); ok {
		return rows, true
	}
	if rows, ok := homelabRows(kind, data, results, ctx); ok {
		return rows, true
	}
	today := parseToday(ctx.Today)
	service := enums.ServiceType(ctx.Service)

	switch {
	case kind == TableOpenInvoices && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, i := range metrics.NinjaOpenInvoices(data.(*sources.NinjaDataset), today) {
			rows = append(rows, Row{[]any{i.Number, i.Client, i.DueDate, i.OverdueDays, i.Balance}})
		}
		return rows, true

	case kind == TableClientShares && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, s := range metrics.NinjaShares(data.(*sources.NinjaDataset), today) {
			rows = append(rows, Row{[]any{s.Client, s.Net, s.Share}})
		}
		return rows, true

	case kind == TableUnbilled && service == enums.ServiceKimai:
		var rows []Row
		for _, g := range metrics.KimaiUnbilled(data.(*sources.KimaiDataset), today) {
			rows = append(rows, Row{[]any{g.Customer, float64(g.Minutes) / minutesPerHourInsight, g.Amount, g.Oldest}})
		}
		return rows, true

	case kind == TableBudgets && service == enums.ServiceKimai:
		var rows []Row
		for _, b := range kimaiBudgets(data.(*sources.KimaiDataset), today) {
			rows = append(rows, Row{[]any{b.Name, b.Pct}})
		}
		return rows, true

	case kind == TableAssetDates && service == enums.ServiceSnipeIT:
		var rows []Row
		for _, i := range metrics.SnipeUpcomingDates(data.(*sources.SnipeDataset), today, 0) {
			rows = append(rows, Row{[]any{i.Name, i.Kind, i.Date}})
		}
		return rows, true

	case kind == TableRates && service == enums.ServiceInvoiceNinja:
		kimai, ok := results[peerKimai].(*sources.KimaiDataset)
		if !ok {
			return nil, false
		}
		rates, _ := metrics.EffectiveRates(kimai, data.(*sources.NinjaDataset), today)
		var rows []Row
		for _, r := range rates {
			rows = append(rows, Row{[]any{r.Customer, r.Hours, r.Net, r.Rate}})
		}
		return rows, true

	case kind == TableMorale && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, m := range metrics.PaymentMorale(data.(*sources.NinjaDataset), moraleSlower, metrics.CenterOf(ctx.Settings)) {
			rows = append(rows, Row{[]any{m.Client, m.UsualDays, m.RecentDays, m.Count}})
		}
		return rows, true

	case kind == TableAppUsage && service == enums.ServiceAuthentik:
		var rows []Row
		for _, a := range data.(*sources.AuthentikDataset).Apps {
			rows = append(rows, Row{[]any{a.Name, a.Events, a.Users}})
		}
		return rows, true

	case kind == TableTrips && service == enums.ServiceDawarich:
		mapping := metrics.ParseAreaMapping(ctx.Options)
		start := metrics.AddMonths(today, -1)
		var rows []Row
		for _, t := range metrics.Trips(data.(*sources.DawarichDataset), mapping, start, today) {
			rows = append(rows, Row{[]any{t.Area, t.Day, t.KM, float64(t.AwayMin) / minutesPerHourInsight}})
		}
		return rows, true
	}
	return nil, false
}

// budgetRow is one Kimai project's budget usage.
type budgetRow struct {
	Name    string
	Pct     float64
	Monthly bool // a monthly time budget: the month is its period
}

// kimaiBudgets: money budgets use their running total, monthly time
// budgets are recomputed from this month's timesheets.
func kimaiBudgets(data *sources.KimaiDataset, today time.Time) []budgetRow {
	var rows []budgetRow
	for _, p := range data.Projects {
		switch {
		case p.Budget > 0:
			rows = append(rows, budgetRow{Name: p.Name, Pct: p.UsedMoney / p.Budget})
		case p.TimeBudgetMin > 0:
			used := p.UsedMinutes
			if p.BudgetType == "month" {
				start := metrics.MonthStart(today)
				used = 0
				for _, s := range data.Timesheets {
					if s.ProjectID != p.ID {
						continue
					}
					if d, ok := metrics.ParseDay(s.Begin); ok && !d.Before(start) {
						used += s.Minutes
					}
				}
			}
			rows = append(rows, budgetRow{Name: p.Name, Pct: float64(used) / float64(p.TimeBudgetMin), Monthly: p.BudgetType == "month"})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Pct > rows[j].Pct })
	return rows
}

func tableView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(TableConfig)
	rows, ok := tableRows(cfg.Table, results, ctx)
	if !ok {
		if _, hasData := results["data"]; hasData {
			return map[string]any{"Unsupported": true}
		}
		return map[string]any{}
	}
	cols := colsFor(cfg.Table)
	sortRows(rows, cols, cfg.Sort)
	var sum Row
	if cfg.SumRow {
		sum = sumRow(rows, cols)
	}
	cols, rows, sum = hideCols(cols, rows, sum, cfg.HideCols)
	total := len(rows)
	if total > cfg.Limit {
		rows = rows[:cfg.Limit]
	}
	out := map[string]any{"Cols": cols, "Rows": rows, "Total": total, "More": total - len(rows)}
	if cfg.SumRow {
		out["Sum"] = sum
	}
	return out
}

// Table sort orders.
const (
	sortAmountDesc = "amount_desc"
	sortAmountAsc  = "amount_asc"
	sortName       = "name"
	sortDate       = "date"
)

// sortRows orders rows by the first column of the kind the order needs.
func sortRows(rows []Row, cols []Col, order string) {
	find := func(formats ...string) int {
		for i, c := range cols {
			if slices.Contains(formats, c.Format) {
				return i
			}
		}
		return -1
	}
	switch order {
	case sortAmountDesc, sortAmountAsc:
		i := find("money", "hours", "km", "pct")
		if i < 0 {
			return
		}
		sort.SliceStable(rows, func(a, b int) bool {
			x, y := asFloat(rows[a].Values[i]), asFloat(rows[b].Values[i])
			if order == sortAmountAsc {
				return x < y
			}
			return x > y
		})
	case sortName, sortDate:
		i := find("text")
		if order == sortDate {
			i = find("day")
		}
		if i < 0 {
			return
		}
		sort.SliceStable(rows, func(a, b int) bool {
			return strings.ToLower(fmt.Sprint(rows[a].Values[i])) < strings.ToLower(fmt.Sprint(rows[b].Values[i]))
		})
	}
}

// summed are the column formats a sum row adds up.
var summed = map[string]bool{"money": true, "hours": true, "km": true}

// sumRow adds up the summable columns of all rows ("" elsewhere).
func sumRow(rows []Row, cols []Col) Row {
	out := Row{Values: make([]any, len(cols))}
	for i, c := range cols {
		if !summed[c.Format] {
			out.Values[i] = ""
			continue
		}
		total := 0.0
		for _, r := range rows {
			total += asFloat(r.Values[i])
		}
		out.Values[i] = total
	}
	return out
}

// hideCols drops the columns named in hide, by key or by their name in
// German or English.
func hideCols(cols []Col, rows []Row, sum Row, hide []string) ([]Col, []Row, Row) {
	if len(hide) == 0 {
		return cols, rows, sum
	}
	var keep []int
	var kept []Col
	for i, c := range cols {
		names := []string{c.Label, strings.ToLower(i18n.T("col."+c.Label, enums.LocaleDE, nil)), strings.ToLower(i18n.T("col."+c.Label, enums.LocaleEN, nil))}
		if slices.ContainsFunc(names, func(n string) bool { return slices.Contains(hide, n) }) {
			continue
		}
		keep = append(keep, i)
		kept = append(kept, c)
	}
	pick := func(r Row) Row {
		if r.Values == nil {
			return r
		}
		out := Row{Values: make([]any, len(keep))}
		for j, i := range keep {
			out.Values[j] = r.Values[i]
		}
		return out
	}
	for i := range rows {
		rows[i] = pick(rows[i])
	}
	return kept, rows, pick(sum)
}

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

func chartView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(ChartConfig)
	data, ok := results["data"]
	if !ok || data == nil {
		return map[string]any{}
	}
	today := parseToday(ctx.Today)
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

// shape fills in the drawn bar and its colour from Pct and the share of
// the period that has passed (soll, 0 if unknown).
func (p *ProgressItem) shape(kind meterKind, soll, slack float64) {
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
	case p.Pct >= 1:
		p.Tier = "red"
	case soll > 0 && p.Pct > soll+slack, soll == 0 && p.Pct >= budgetWarn:
		p.Tier = "yellow"
	default:
		p.Tier = "green"
	}
}

// budgetWarn colours a budget without a period from this share on.
const budgetWarn = 0.8

// passed is the share of the period around today that is over (today counted).
func passed(today time.Time, start, end time.Time) float64 {
	return float64(today.Sub(start).Hours()/24+1) / float64(end.Sub(start).Hours()/24)
}

func progressView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(ProgressConfig)
	data, ok := results["data"]
	if !ok || data == nil {
		return map[string]any{}
	}
	today := parseToday(ctx.Today)
	var items []ProgressItem

	if enums.ServiceType(ctx.Service) == enums.ServiceKimai {
		for _, b := range kimaiBudgets(data.(*sources.KimaiDataset), today) {
			if len(cfg.Projects) > 0 && !slices.Contains(cfg.Projects, strings.ToLower(b.Name)) {
				continue
			}
			item := ProgressItem{Label: b.Name, Pct: b.Pct}
			soll := 0.0
			if b.Monthly {
				start := metrics.MonthStart(today)
				soll = passed(today, start, metrics.AddMonths(start, 1))
			}
			item.shape(meterBudget, soll, cfg.Warn)
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
		item.shape(meterGoal, passed(today, start, start.AddDate(1, 0, 0)), cfg.Warn)
		if !cfg.Soll {
			item.Soll, item.SollPct = 0, 0
		}
		items = append(items, item)
	}
	return map[string]any{"Items": items}
}

// ── Deadlines ──

func deadlinesView(cfgAny any, _ map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(DeadlinesConfig)
	tax, configured := metrics.ParseTaxSettings(ctx.Settings)
	today := parseToday(ctx.Today)

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

func trendView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(TrendConfig)
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

func dataQuery(any) []Query {
	return []Query{{Name: "data", Source: "data", Conn: ConnWidget}}
}

// peerKimai names the space's Kimai dataset for rate views.
const peerKimai = "kimai"

var kimaiPeer = Query{Name: peerKimai, Source: "data", Conn: ConnPeer, Service: enums.ServiceKimai}

// peerSure names the space's Sure dataset (recurring costs).
const peerSure = "sure"

var surePeer = Query{Name: peerSure, Source: "data", Conn: ConnPeer, Service: enums.ServiceSure}

func kpiQueries(cfg any) []Query {
	switch cfg.(KpiConfig).Metric {
	case MetricEffectiveRate:
		return append(dataQuery(nil), kimaiPeer)
	case MetricLiquidity30, MetricSafeToSpend:
		return append(dataQuery(nil), surePeer)
	}
	return dataQuery(nil)
}

func tableQueries(cfg any) []Query {
	if cfg.(TableConfig).Table == TableRates {
		return append(dataQuery(nil), kimaiPeer)
	}
	kind := cfg.(TableConfig).Table
	return append(append(dataQuery(nil), crossQueries(kind)...), homelabQueries(kind)...)
}

func init() {
	Register(WidgetType{Key: "kpi", Decode: decodeKpi, Template: "widgets/kpi", Category: CategoryInsight,
		RefreshS: 600, Queries: kpiQueries, View: kpiView})
	Register(WidgetType{Key: "table", Decode: decodeTable, Template: "widgets/table", Category: CategoryInsight,
		RefreshS: 600, Queries: tableQueries, View: tableView})
	Register(WidgetType{Key: "chart", Decode: decodeChart, Template: "widgets/chart", Category: CategoryInsight,
		RefreshS: 3600, Queries: dataQuery, View: chartView})
	Register(WidgetType{Key: "progress", Decode: decodeProgress, Template: "widgets/progress", Category: CategoryInsight,
		RefreshS: 600, Queries: dataQuery, View: progressView})
	Register(WidgetType{Key: "deadlines", Decode: decodeDeadlines, Template: "widgets/deadlines", Category: CategoryInsight,
		RefreshS: 3600, View: deadlinesView})
	Register(WidgetType{Key: "trend", Decode: decodeTrend, Template: "widgets/trend", Category: CategoryInsight,
		RefreshS: 3600, View: trendView, Extra: ExtraPoints})
	Register(WidgetType{Key: "updates", Decode: decodeTopic(rules.TopicUpdates), Template: "widgets/topic", Category: CategoryInsight,
		RefreshS: 600, Extra: ExtraHints})
	Register(WidgetType{Key: "hints", Decode: decodeHints, Template: "widgets/hints", Category: CategoryInsight,
		RefreshS: 300, Extra: ExtraHints})
	Register(WidgetType{Key: "expiries", Decode: decodeExpiries, Template: "widgets/expiries", Category: CategoryInsight,
		RefreshS: 3600, Extra: ExtraHints})
}
