package sources

// Ghostfolio: the depot's value, what went in, its performance and the
// value per day. The secret is the user's security token; Ghostfolio
// trades it for a session token.
//
//	POST api/v1/auth/anonymous {accessToken}             → {authToken}
//	GET  api/v2/portfolio/performance?range=1y (Bearer)  → {performance{currentValueInBaseCurrency, totalInvestment, netPerformancePercentage},
//	                                                        chart[{date, value, netPerformance}]}
//
// Ghostfolio has no month range (1d 1y 5y max mtd wtd ytd <year>): Andon
// asks for a year and keeps the last depotDays. Their performance is the
// gain without deposits over the value at the start:
//
//	(netPerformance today − netPerformance 30 days ago) / value 30 days ago
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

const (
	// depotRange is the chart Ghostfolio returns; depotDays of it stay.
	depotRange = "1y"
	depotDays  = 30
)

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
	perf, err := api.Get(ctx, "api/v2/portfolio/performance", url.Values{"range": {depotRange}})
	if err != nil {
		return nil, fetchError(err)
	}
	m := asMap(perf)
	p := asMap(m["performance"])
	data := &GhostfolioDataset{URL: sctx.URL, Value: firstNumber(p, "currentValueInBaseCurrency", "currentValue", "currentNetWorth"),
		Investment: asFloat(p["totalInvestment"]), PerformancePct: asFloat(p["netPerformancePercentage"]) * 100}
	data.Days, data.PerformancePct = depotChart(asList(m["chart"]), data.PerformancePct, time.Now().UTC())
	if hold, err := api.Get(ctx, "api/v1/portfolio/holdings", nil); err == nil {
		for _, raw := range asList(asMap(hold)["holdings"]) {
			h := asMap(raw)
			data.Holdings = append(data.Holdings, Holding{Name: asStr(h["name"]), Value: firstNumber(h, "valueInBaseCurrency", "value")})
		}
		sort.Slice(data.Holdings, func(i, j int) bool { return data.Holdings[i].Value > data.Holdings[j].Value })
	}
	return data, nil
}

// depotChart keeps the chart's last depotDays and their performance in
// percent; pct stays when the chart is too short to tell.
func depotChart(chart []any, pct float64, now time.Time) ([]DepotDay, float64) {
	from := now.AddDate(0, 0, -depotDays).Format(time.DateOnly)
	var days []DepotDay
	var gains []float64
	for _, raw := range chart {
		c := asMap(raw)
		if asStr(c["date"]) < from {
			continue
		}
		days = append(days, DepotDay{Date: asStr(c["date"]), Value: firstNumber(c, "valueInBaseCurrency", "value", "netWorth")})
		gains = append(gains, asFloat(c["netPerformance"]))
	}

	if len(days) < 2 || days[0].Value == 0 {
		return days, pct
	}
	return days, (gains[len(gains)-1] - gains[0]) / days[0].Value * 100
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
