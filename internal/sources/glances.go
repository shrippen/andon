package sources

// Glances history for charts: one metric's recent values.
//
//	GET /api/4/cpu/total/history/60 → {"total": [["2026-09-25T10:00:00", 12.5], …]}

import (
	"context"
	"strconv"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// GlancesMetrics maps a chart metric to its plugin and field.
var GlancesMetrics = map[string][2]string{
	"cpu":  {"cpu", "total"},
	"mem":  {"mem", "percent"},
	"load": {"load", "min5"},
	"swap": {"memswap", "percent"},
}

// Sample is one value at one time.
type Sample struct {
	At    string
	Value float64
}

// GlancesHistory is one metric over time, oldest first.
type GlancesHistory struct {
	Metric  string
	Samples []Sample
}

var GlancesHistorySource = source{key: "glances_history", ttl: glancesTTL, service: enums.ServiceGlances, fetch: fetchGlancesHistory}

func fetchGlancesHistory(ctx context.Context, sctx Ctx) (any, error) {
	metric := asStr(sctx.Params["metric"])
	target, ok := GlancesMetrics[metric]
	if !ok {
		return nil, newSourceError("unknown metric %s", metric)
	}
	points := int(asFloat(sctx.Params["points"]))
	if isDemo(sctx) {
		return DemoGlancesHistory(time.Now(), metric, points), nil
	}
	api := services.GlancesApi{URL: sctx.URL, Token: sctx.Secret, Verify: sctx.VerifyTLS, Version: int(asFloat(sctx.Options["api_version"]))}
	body, err := api.Get(ctx, target[0]+"/"+target[1]+"/history/"+strconv.Itoa(points))
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}

	out := &GlancesHistory{Metric: metric}
	for _, raw := range asList(asMap(body)[target[1]]) {
		pair := asList(raw)
		if len(pair) != 2 {
			continue
		}
		out.Samples = append(out.Samples, Sample{At: asStr(pair[0]), Value: asFloat(pair[1])})
	}
	return out, nil
}

func init() {
	Register(GlancesHistorySource)
}
