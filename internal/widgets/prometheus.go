package widgets

// "prometheus_value": one PromQL expression as a number, with its last
// hours as a spark line; the dialog draws them with axis and hover, and
// lists the server's firing alerts.
//
//	query "node_load1", hours 24, warn 3 ─► 3,4 (yellow) + spark of 24 values

import (
	"math"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// PromValueConfig is the "prometheus_value" widget's config.
type PromValueConfig struct {
	Query    string
	Unit     string
	Hours    int
	Digits   int
	Warn     float64
	HasWarn  bool
	Inverted bool // below Warn is bad (free space), not above (load)
}

const promValueName = "value"

// promHours are the history choices in hours.
var promHours = map[string]int{"none": 0, "6h": 6, "24h": 24, "7d": 7 * 24}

// promDefaultQuery counts the targets that answer: a value every server has.
const promDefaultQuery = "sum(up)"

func init() {
	Tile[PromValueConfig]{Key: "prometheus_value", Detail: promValueDetail, DetailQueries: promAlertsQuery, Category: CategoryInsight, Topic: TopicHomelab,
		Service: enums.ServicePrometheus, RefreshS: 300,
		Fields: []Field{{Key: "query", Input: InputText, Default: promDefaultQuery}, {Key: "unit", Input: InputText}, sel("hours", "24h", "none", "6h", "24h", "7d"),
			{Key: "digits", Input: InputNumber, Default: 1, Min: "0", Max: "3"}, {Key: "warn", Input: InputText}, {Key: "invert", Input: InputCheck}},
		Decode: decodePromValue,
		Queries: func(cfg PromValueConfig) []Query {
			return []Query{{Name: promValueName, Source: "prometheus.query", Conn: ConnWidget, Params: map[string]any{"query": cfg.Query, "hours": float64(cfg.Hours)}}}
		},
		View: promValueView,
		Calm: func(v map[string]any) bool { return v["Found"] == true && v["Warned"] == false }}.add()
}

func decodePromValue(r Raw) PromValueConfig {
	cfg := PromValueConfig{Query: r.String("query"), Unit: r.String("unit"), Hours: promHours[r.Pick("hours")], Digits: r.Int("digits"), Inverted: r.Bool("invert")}
	if warn, err := strconv.ParseFloat(r.String("warn"), 64); err == nil {
		cfg.Warn, cfg.HasWarn = warn, true
	}
	return cfg
}

// promAlertsQuery reads the server's alerts when the dialog opens.
func promAlertsQuery(PromValueConfig) []Query { return dataQuery(nil) }

// warned: the value crossed the warning line.
func (c PromValueConfig) warned(v float64) bool {
	if !c.HasWarn {
		return false
	}
	if c.Inverted {
		return v <= c.Warn
	}
	return v >= c.Warn
}

// promPoints are the hourly values that exist, oldest first.
func promPoints(hourly []*float64) []float64 {
	var out []float64
	for _, v := range hourly {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}

func promValueView(cfg PromValueConfig, results map[string]any, _ ViewCtx) map[string]any {
	v, ok := results[promValueName].(*sources.PromValue)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{"Found": v.Found, "Value": v.Value, "Unit": cfg.Unit, "Digits": cfg.Digits, "Warned": v.Found && cfg.warned(v.Value), "Warn": cfg.Warn, "Inverted": cfg.Inverted,
		"Spark": SparkOf(promPoints(v.Hourly)), "Hours": cfg.Hours, "Series": v.Series}
}

func promValueDetail(cfg PromValueConfig, results map[string]any, _ ViewCtx) DetailView {
	body := &DetailBody{Side: []Fact{{Label: T("prometheus.query"), Value: cfg.Query}}}
	if v, ok := results[promValueName].(*sources.PromValue); ok && v.Found {
		body.Facts = []Kpi{{Value: NumU(v.Value, cfg.Digits, cfg.Unit), Label: T("prometheus.now"), Tier: tierIf(cfg.warned(v.Value), "yellow", "")}}
		if points := promPoints(v.Hourly); len(points) > 0 {
			low, high, sum := math.Inf(1), math.Inf(-1), 0.0
			for _, p := range points {
				low, high, sum = min(low, p), max(high, p), sum+p
			}
			body.Facts = append(body.Facts, Kpi{Value: NumU(low, cfg.Digits, cfg.Unit), Label: T("prometheus.min")},
				Kpi{Value: NumU(sum/float64(len(points)), cfg.Digits, cfg.Unit), Label: T("prometheus.avg")},
				Kpi{Value: NumU(high, cfg.Digits, cfg.Unit), Label: T("prometheus.max")})
			body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: Text{Key: "prometheus.hours", Args: map[string]any{"n": cfg.Hours}}, Hero: true, Data: promGraph(cfg, v.Hourly)})
		}
		if v.Series > 1 {
			body.Side = append(body.Side, Fact{Label: T("prometheus.series"), Value: v.Series})
		}
	}
	if data, ok := results[openName].(*sources.PrometheusDataset); ok {
		body.Blocks = append(body.Blocks, promAlertBlock(data))
	}
	return DetailView{Body: body}
}

// promGraph is the hourly line with its hours as x labels and the
// warning line as goal.
func promGraph(cfg PromValueConfig, hourly []*float64) Graph {
	values := make([]float64, len(hourly))
	labels := make([]any, len(hourly))
	start := time.Now().In(clockZone()).Truncate(time.Hour).Add(-time.Duration(len(hourly)-1) * time.Hour)
	for i, v := range hourly {
		values[i] = Gap
		if v != nil {
			values[i] = *v
		}
		labels[i] = start.Add(time.Duration(i) * time.Hour).Format(timeOfDay)
	}
	g := LineGraph(Series{Values: values, Class: "s1"})
	g.Unit, g.Labels = cfg.Unit, labels
	g.Ticks = []any{labels[0], Txt("detail.now")}
	if cfg.HasWarn {
		g.Goal, g.HasGoal, g.GoalDanger = cfg.Warn, true, true
	}
	return g
}

// promAlertBlock lists the firing alerts, most severe first.
func promAlertBlock(data *sources.PrometheusDataset) Block {
	var rows [][]Cell
	for _, a := range data.Alerts {
		if !a.Firing() {
			continue
		}
		state := map[string]string{"critical": "bad", "warning": "warn"}[a.Severity]
		rows = append(rows, []Cell{{Value: a.Name, State: state}, {Value: a.Instance}, {Value: a.Summary}, {Value: agoOf(a.Since)}})
	}
	if len(rows) == 0 {
		return Block{Kind: BlockText, Label: T("prometheus.alerts"), Data: Txt("prometheus.no_alerts")}
	}
	return Block{Kind: BlockTable, Label: T("prometheus.alerts"), Meta: len(rows), Data: Table{
		Head: []Text{T("prometheus.col.alert"), T("prometheus.col.instance"), T("prometheus.col.summary"), T("prometheus.col.since")}, Rows: rows}}
}
