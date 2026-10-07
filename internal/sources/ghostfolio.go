package sources

// Ghostfolio: the depot's value, what went in, its performance and the
// value per day. The secret is the user's security token; Ghostfolio
// trades it for a session token.
//
//	POST api/v1/auth/anonymous {accessToken}             → {authToken}
//	GET  api/v2/portfolio/performance?range=1m (Bearer)  → {performance{currentValueInBaseCurrency, totalInvestment, netPerformancePercentage},
//	                                                        chart[{date, value…}]}
//	GET  api/v1/portfolio/holdings                       → {holdings[{name, valueInBaseCurrency}]}

import (
	"context"
	"net/url"
	"sort"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// DepotDay is the depot's value on one day.
type DepotDay struct {
	Date  string // "2026-10-07"
	Value float64
}

// Holding is one position.
type Holding struct {
	Name  string
	Value float64
}

// GhostfolioDataset is the depot.
type GhostfolioDataset struct {
	URL            string
	Currency       string
	Value          float64
	Investment     float64
	PerformancePct float64 // over the chart's range
	Days           []DepotDay
	Holdings       []Holding // largest first
}

var GhostfolioData = source{key: "ghostfolio.data", ttl: opsTTL, service: enums.ServiceGhostfolio, fetch: fetchGhostfolio}

func fetchGhostfolio(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGhostfolio(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	login := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{}, Verify: sctx.VerifyTLS}
	raw, err := login.Post(ctx, "api/v1/auth/anonymous", map[string]any{"accessToken": secret})
	if err != nil {
		return nil, fetchError(err)
	}
	token := asStr(asMap(raw)["authToken"])
	if token == "" {
		return nil, fetchError(services.ErrLogin)
	}
	api := services.BearerApi(sctx.URL, token, sctx.TLS())
	perf, err := api.Get(ctx, "api/v2/portfolio/performance", url.Values{"range": {"1m"}})
	if err != nil {
		return nil, fetchError(err)
	}
	m := asMap(perf)
	p := asMap(m["performance"])
	data := &GhostfolioDataset{URL: sctx.URL, Value: firstNumber(p, "currentValueInBaseCurrency", "currentValue", "currentNetWorth"),
		Investment: asFloat(p["totalInvestment"]), PerformancePct: asFloat(p["netPerformancePercentage"]) * 100}
	for _, raw := range asList(m["chart"]) {
		c := asMap(raw)
		data.Days = append(data.Days, DepotDay{Date: asStr(c["date"]), Value: firstNumber(c, "valueInBaseCurrency", "value", "netWorth")})
	}
	if hold, err := api.Get(ctx, "api/v1/portfolio/holdings", nil); err == nil {
		for _, raw := range asList(asMap(hold)["holdings"]) {
			h := asMap(raw)
			data.Holdings = append(data.Holdings, Holding{Name: asStr(h["name"]), Value: firstNumber(h, "valueInBaseCurrency", "value")})
		}
		sort.Slice(data.Holdings, func(i, j int) bool { return data.Holdings[i].Value > data.Holdings[j].Value })
	}
	return data, nil
}

// firstNumber reads the first of keys that holds a number; Ghostfolio
// renamed its fields between versions.
func firstNumber(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return asFloat(v)
		}
	}
	return 0
}

func DemoGhostfolio(now time.Time) *GhostfolioDataset {
	data := &GhostfolioDataset{}
	demoworld.MustDecode("depot", now, data)
	return data
}

func init() {
	Register(GhostfolioData)
	Register(testOf{GhostfolioData, func(d any) map[string]any { return map[string]any{"value": d.(*GhostfolioDataset).Value} }})
}
