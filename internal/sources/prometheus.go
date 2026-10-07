package sources

// Prometheus: its firing alerts become hints, and a tile reads one PromQL
// expression as a value or an hourly line. No login, a bearer token, or
// "user:password" for basic auth in front of it.
//
//	GET api/v1/alerts                         → {"data": {"alerts": [{labels, annotations, state, activeAt}]}}
//	GET api/v1/query?query=…                  → the value now
//	GET api/v1/query_range?query=…&step=3600  → one value per hour

import (
	"context"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	promAlerts     = "api/v1/alerts"
	promQuery      = "api/v1/query"
	promRange      = "api/v1/query_range"
	promStep       = time.Hour
	promMaxHours   = 7 * 24
	promFiring     = "firing"
	promQueryTTL   = 5 * time.Minute
	promSeverity   = "severity"
	promInstance   = "instance"
	promSummary    = "summary"
	promDescriptor = "description"
)

// PromAlert is one alert that is firing or pending.
type PromAlert struct {
	Name     string // alertname
	Severity string // the severity label: critical, warning, info …
	Instance string // the instance label: "nas:9100"
	Summary  string // annotation summary, else description
	State    string // firing, pending
	Since    time.Time
}

// Firing reports whether the alert fires (not only pending).
func (a PromAlert) Firing() bool { return a.State == promFiring }

// PrometheusDataset is the server's alerts.
type PrometheusDataset struct {
	URL    string
	Alerts []PromAlert
}

// PromValue is one expression: its value now and, if asked, one value per
// hour (oldest first; a missing hour is nil, so it stores as JSON null).
type PromValue struct {
	Value  float64
	Found  bool // the expression returned a series
	Series int  // how many series it returned; the value is the first's
	Hourly []*float64
}

var PrometheusData = source{key: "prometheus.data", ttl: dataTTL, service: enums.ServicePrometheus, fetch: fetchPrometheus}

// PrometheusQuery reads params query and hours (0 = the value only).
var PrometheusQuery = source{key: "prometheus.query", ttl: promQueryTTL, service: enums.ServicePrometheus, fetch: fetchPromQuery}

// promApi logs in as the secret says: "" none, "user:pass" basic, else bearer.
func promApi(sctx Ctx) services.KeyedApi {
	if strings.Contains(sctx.Secret, ":") {
		return services.BasicApi(sctx.URL, sctx.Secret, sctx.TLS())
	}
	return services.BearerApi(sctx.URL, sctx.Secret, sctx.TLS())
}

func fetchPrometheus(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPrometheus(time.Now().UTC()), nil
	}
	raw, err := promApi(sctx).Get(ctx, promAlerts, nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &PrometheusDataset{URL: sctx.URL}
	for _, item := range asList(asMap(asMap(raw)["data"])["alerts"]) {
		a := asMap(item)
		labels, notes := asMap(a["labels"]), asMap(a["annotations"])
		summary := asStr(notes[promSummary])
		if summary == "" {
			summary = asStr(notes[promDescriptor])
		}
		data.Alerts = append(data.Alerts, PromAlert{Name: asStr(labels["alertname"]), Severity: strings.ToLower(asStr(labels[promSeverity])),
			Instance: asStr(labels[promInstance]), Summary: summary, State: asStr(a["state"]), Since: parseTime(a["activeAt"])})
	}
	return data, nil
}

func fetchPromQuery(ctx context.Context, sctx Ctx) (any, error) {
	query := strings.TrimSpace(asStr(sctx.Params["query"]))
	hours := min(int(asFloat(sctx.Params["hours"])), promMaxHours)
	if isDemo(sctx) {
		return DemoPromQuery(time.Now().UTC(), query, hours), nil
	}
	if query == "" {
		return nil, newSourceError("prometheus.no_query")
	}
	api := promApi(sctx)
	raw, err := api.Get(ctx, promQuery, url.Values{"query": {query}})
	if err != nil {
		return nil, fetchError(err)
	}
	out := &PromValue{}
	result := asList(asMap(asMap(raw)["data"])["result"])
	out.Series = len(result)
	if len(result) > 0 {
		out.Value, out.Found = sampleValue(asList(asMap(result[0])["value"]))
	}
	if hours <= 0 {
		return out, nil
	}

	end := time.Now().UTC().Truncate(promStep)
	start := end.Add(-time.Duration(hours-1) * promStep)
	raw, err = api.Get(ctx, promRange, url.Values{"query": {query}, "start": {unixText(start)}, "end": {unixText(end)}, "step": {strconv.Itoa(int(promStep.Seconds()))}})
	if err != nil {
		return nil, fetchError(err)
	}
	out.Hourly = make([]*float64, hours)
	if series := asList(asMap(asMap(raw)["data"])["result"]); len(series) > 0 {
		for _, s := range asList(asMap(series[0])["values"]) {
			pair := asList(s)
			v, ok := sampleValue(pair)
			if !ok {
				continue
			}
			at := time.Unix(int64(asFloat(pair[0])), 0).UTC()
			if i := int(at.Sub(start) / promStep); i >= 0 && i < hours {
				out.Hourly[i] = &v
			}
		}
	}
	return out, nil
}

// sampleValue reads a sample [unix time, "value"].
func sampleValue(pair []any) (float64, bool) {
	if len(pair) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(asStr(pair[1]), 64)
	return v, err == nil
}

func unixText(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

// DemoPrometheus is Studio Weber's alerts.
func DemoPrometheus(now time.Time) *PrometheusDataset {
	var p struct {
		URL    string
		Alerts []PromAlert
	}
	demoworld.MustDecode("prometheus", now, &p)
	data := &PrometheusDataset{URL: p.URL}
	for _, a := range p.Alerts {
		a.State = promFiring
		data.Alerts = append(data.Alerts, a)
	}
	return data
}

// DemoPromQuery answers the queries the world names, the first one for
// any other expression.
func DemoPromQuery(now time.Time, query string, hours int) *PromValue {
	var p struct {
		Queries map[string]struct {
			Value  float64
			Hourly []float64
		}
	}
	demoworld.MustDecode("prometheus", now, &p)
	// The world's keys lose their underscores when decoded: node_load1 → nodeload1.
	q, ok := p.Queries[strings.ReplaceAll(query, "_", "")]
	if !ok {
		names := slices.Sorted(maps.Keys(p.Queries))
		q = p.Queries[names[0]]
	}
	out := &PromValue{Value: q.Value, Found: true, Series: 1}
	if hours > 0 {
		for _, v := range q.Hourly[max(len(q.Hourly)-hours, 0):] {
			out.Hourly = append(out.Hourly, &v)
		}
	}
	return out
}

func init() {
	Register(PrometheusData)
	Register(PrometheusQuery)
	Register(testOf{PrometheusData, func(d any) map[string]any { return map[string]any{"alerts": len(d.(*PrometheusDataset).Alerts)} }})
}
