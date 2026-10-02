package widgets

// "rate_trend": what an hour earned per month, revenue (Invoice Ninja)
// over all tracked hours (Kimai), with the target rate.
//
//	86 €/h  ▲ 4 €       ← last complete month vs the one before
//	╱╲_╱‾╲_╱‾           ← 12 complete months

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// RateTrendConfig is the "rate_trend" widget's config; Target 0 = none.
type RateTrendConfig struct {
	Target       float64
	Months       int
	BillableOnly bool // the rate over billable hours only
}

func rateTrendView(cfg RateTrendConfig, results map[string]any, ctx ViewCtx) map[string]any {
	ninja, ok := results["data"].(*sources.NinjaDataset)
	kimai, ok2 := results[peerKimai].(*sources.KimaiDataset)
	if !ok || !ok2 {
		return map[string]any{}
	}
	lastMonth := metrics.AddMonths(todayOf(ctx), -1)
	kind := metrics.HoursAll
	if cfg.BillableOnly {
		kind = metrics.HoursBillable
	}
	hours := kimaiMonthHoursOf(kimai, lastMonth, cfg.Months, kind)
	var rates []float64
	for i, m := range metrics.NinjaByMonth(ninja, lastMonth, cfg.Months) {
		if i < len(hours) && hours[i] > 0 {
			rates = append(rates, m.Net/hours[i])
		}
	}
	out := map[string]any{"Currency": ninja.Currency, "Target": cfg.Target, "Month": lastMonth.Format("01/2006"), "Months": cfg.Months}
	if len(rates) == 0 {
		return out
	}
	out["Rate"] = rates[len(rates)-1]
	if len(rates) > 1 {
		out["Prev"] = rates[len(rates)-2]
		out["Delta"] = rates[len(rates)-1] - rates[len(rates)-2]
	}
	out["Spark"] = SparkOf(rates)
	return out
}

func init() {
	Tile[RateTrendConfig]{Key: "rate_trend", Detail: rateTrendDetail, Category: CategoryInsight, Topic: TopicAnalysis, Service: enums.ServiceInvoiceNinja, RefreshS: 3600,
		Fields: []Field{{Key: "target_value", Input: InputNumber, Min: "0"}, {Key: "months", Input: InputNumber, Default: sparkMonths, Min: "3", Max: "36"},
			{Key: "billable_only", Input: InputCheck}},
		Renames: []rename{{from: "target", to: "target_value"}},
		Decode: func(r Raw) RateTrendConfig {
			return RateTrendConfig{Target: r.Float("target_value"), Months: r.Int("months"), BillableOnly: r.Bool("billable_only")}
		},
		Queries: func(RateTrendConfig) []Query { return append(dataQuery(nil), kimaiPeer) }, View: rateTrendView}.add()
}
