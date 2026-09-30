package widgets

// "glances_chart": one Glances metric over the last minutes, as a line.

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

const (
	defaultGlancesMetric = "cpu"
	defaultGlancesPoints = 60
	chartMargin          = 5
)

type GlancesChartConfig struct {
	Metric string
	Points int
	Warn   float64 // a horizontal line at this value, 0 = none
}

func decodeGlancesChart(r Raw) GlancesChartConfig {
	return GlancesChartConfig{Metric: r.Pick("metric"), Points: r.Int("points"), Warn: r.Float("warn_line")}
}

// glancesChartView scales samples into the trend chart's box; percent
// metrics keep a fixed 0–100 axis so a quiet host looks quiet.
// glancesBars caps the bars of the load chart.
const glancesBars = 36

func glancesChartView(cfg GlancesChartConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["history"].(*sources.GlancesHistory)
	if !ok || len(data.Samples) < 2 {
		return map[string]any{}
	}
	low, high := 0.0, 100.0
	now := data.Samples[len(data.Samples)-1].Value
	if data.Metric == "load" {
		high = 0
		for _, s := range data.Samples {
			high = max(high, s.Value)
		}
		high = max(high, 1, cfg.Warn)
	}

	step := float64(trendWidth) / float64(len(data.Samples)-1)
	coords := make([]string, len(data.Samples))
	for i, s := range data.Samples {
		y := trendHeight - (min(s.Value, high)-low)/(high-low)*(trendHeight-2*chartMargin) - chartMargin
		coords[i] = formatPoint(float64(i)*step, y)
	}
	values := make([]float64, len(data.Samples))
	for i, s := range data.Samples {
		values[i] = s.Value
	}
	out := map[string]any{"Path": "M" + joinPoints(coords), "Bars": barsOf(values, glancesBars, high), "Now": now, "High": high, "Metric": data.Metric,
		"W": trendWidth, "H": trendHeight, "First": data.Samples[0].At, "Last": data.Samples[len(data.Samples)-1].At}
	if cfg.Warn > 0 && cfg.Warn <= high {
		out["Warn"], out["WarnPct"] = cfg.Warn, pctOf(cfg.Warn, high)
	}
	return out
}

func init() {
	Tile[GlancesChartConfig]{Key: "glances_chart", Category: CategoryStart, Topic: TopicHomelab, Service: enums.ServiceGlances, RefreshS: 60,
		Live: true, DataChoice: true,
		Fields: []Field{sel("metric", defaultGlancesMetric, "cpu", "mem", "load", "swap"),
			{Key: "points", Input: InputNumber, Default: defaultGlancesPoints, Min: "10", Max: "300"}, {Key: "warn_line", Input: InputNumber, Min: "0"}},
		Decode: decodeGlancesChart, View: glancesChartView,
		Queries: func(cfg GlancesChartConfig) []Query {
			return []Query{{Name: "history", Source: "glances_history", Conn: ConnWidget,
				Params: map[string]any{"metric": cfg.Metric, "points": float64(cfg.Points)}}}
		}}.add()
}
