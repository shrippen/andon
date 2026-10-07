package widgets

// "depot": the Ghostfolio depot's value with its last days as a spark;
// the dialog draws the days with axis and hover, what went in, and the
// positions.

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

// DepotConfig is the "depot" widget's config.
type DepotConfig struct{}

func init() {
	Tile[DepotConfig]{Key: "depot", Detail: dataDetail(depotDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceGhostfolio, RefreshS: 3600,
		Decode: func(Raw) DepotConfig { return DepotConfig{} }, Queries: ownData[DepotConfig], View: dataView(depotView)}.add()
}

func depotValues(data *sources.GhostfolioDataset) []float64 {
	out := make([]float64, len(data.Days))
	for i, d := range data.Days {
		out[i] = d.Value
	}
	return out
}

func depotView(_ DepotConfig, data *sources.GhostfolioDataset, _ ViewCtx) map[string]any {
	return map[string]any{"Value": data.Value, "Currency": data.Currency, "Pct": data.PerformancePct, "Up": data.PerformancePct >= 0,
		"Spark": SparkOf(depotValues(data))}
}

func depotDetail(_ DepotConfig, data *sources.GhostfolioDataset, _ ViewCtx, results map[string]any) DetailView {
	body := &DetailBody{Facts: []Kpi{{Value: Money(data.Value, data.Currency), Label: T("depot.value")},
		{Value: Money(data.Investment, data.Currency), Label: T("depot.invested")},
		{Value: NumU(data.PerformancePct, 1, "%"), Label: T("depot.performance"), Tier: tierIf(data.PerformancePct >= 0, "green", "red")}}}
	if len(data.Days) > 1 {
		labels := make([]any, len(data.Days))
		for i, d := range data.Days {
			labels[i] = DayS(d.Date)
		}
		g := LineGraph(Series{Values: depotValues(data), Class: "s1"})
		g.Unit, g.Labels, g.Ticks = data.Currency, labels, []any{labels[0], labels[len(labels)-1]}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("depot.days"), Hero: true, Data: g})
	}
	if len(data.Holdings) > 0 {
		var bars []ShareBar
		for _, h := range data.Holdings {
			bars = append(bars, ShareBar{Name: h.Name, Pct: h.Value / max(data.Value, 1) * percentScale, Value: Money(h.Value, data.Currency)})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("depot.holdings"), Data: bars})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
