package widgets

// Widgets of the network, media and everyday integrations:
//
//	tailscale     devices with online state and key expiry
//	mediaserver   running streams and library size
//	arr_upcoming  next episodes / movies of Sonarr or Radarr
//	grocy         expired and due products, chores
//	dwd           weather warnings
//	github        repos: PRs, issues, CI, latest release
//	speedtest     latest down/up/ping
//	energy        Tibber prices, cheapest hours, cost; power from Home Assistant

import (
	"fmt"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	peerHass       = "hass"
	upcomingShown  = 8
	integrationTTL = 300 // seconds between reloads
	energyDaysCost = 30
)

// EnergyConfig: an optional Home Assistant sensor with the current power.
type EnergyConfig struct{ PowerEntity string }

func decodeEnergy(raw map[string]any) any {
	return EnergyConfig{PowerEntity: asString(raw["power_entity"])}
}

// datasetView passes a service's dataset to the template as is.
func datasetView(_ any, results map[string]any, _ ViewCtx) map[string]any {
	if results["data"] == nil {
		return map[string]any{}
	}
	return map[string]any{"Data": results["data"]}
}

func arrView(_ any, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["data"].(*sources.ArrDataset)
	if !ok {
		return map[string]any{}
	}
	items := data.Upcoming
	if len(items) > upcomingShown {
		items = items[:upcomingShown]
	}
	return map[string]any{"Data": data, "Items": items}
}

// energyView draws today's and tomorrow's prices and finds the cheapest
// hours ahead.
func energyView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["data"].(*sources.TibberDataset)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{"Data": data}

	// Prices hold for a whole hour, so the curve is a staircase:
	//	M0,y0 H40 V y1 H80 V y2 …   plus the cheapest window as a band
	//	and a line for now, on the same x scale.
	now := time.Now().UTC()
	if len(data.Prices) >= 2 {
		low, high := data.Prices[0].Total, data.Prices[0].Total
		for _, p := range data.Prices {
			low, high = min(low, p.Total), max(high, p.Total)
		}
		span := max(high-low, 0.01)
		step := float64(trendWidth) / float64(len(data.Prices))
		y := func(v float64) float64 { return trendHeight - (v-low)/span*(trendHeight-2*chartMargin) - chartMargin }
		path := fmt.Sprintf("M0,%.1f", y(data.Prices[0].Total))
		for i, p := range data.Prices {
			if i > 0 {
				path += fmt.Sprintf(" V%.1f", y(p.Total))
			}
			path += fmt.Sprintf(" H%.1f", float64(i+1)*step)
		}
		first := data.Prices[0].At
		x := func(t time.Time) float64 { return t.Sub(first).Hours() * step }
		out["Path"], out["W"], out["H"], out["Low"], out["High"] = path, trendWidth, trendHeight, low, high
		if nx := x(now); nx > 0 && nx < float64(trendWidth) {
			out["NowX"] = nx
		}
		if start, _, ok := metrics.CheapWindow(data.Prices, now, metrics.CheapHours); ok {
			out["CheapX"], out["CheapW"] = x(start), float64(metrics.CheapHours)*step
		}
	}
	if start, avg, ok := metrics.CheapWindow(data.Prices, now, metrics.CheapHours); ok {
		out["CheapStart"], out["CheapAvg"], out["CheapHours"] = start, avg, metrics.CheapHours
	}

	cost, kwh := 0.0, 0.0
	days := data.Days
	if len(days) > energyDaysCost {
		days = days[len(days)-energyDaysCost:]
	}
	for _, d := range days {
		cost, kwh = cost+d.Cost, kwh+d.KWh
	}
	out["Cost"], out["KWh"], out["Days"] = cost, kwh, len(days)

	cfg := cfgAny.(EnergyConfig)
	if hass, ok := results[peerHass].(*sources.HassDataset); ok && cfg.PowerEntity != "" {
		if e, found := hass.Find(cfg.PowerEntity); found {
			out["Power"], out["PowerUnit"] = e.State, e.Unit
		}
	}
	return out
}

func init() {
	simple := func(key string, service enums.ServiceType, view ViewFunc) {
		Register(WidgetType{Key: key, Decode: decodeEmpty, Template: "widgets/" + key, Category: CategoryInsight,
			Service: service, RefreshS: integrationTTL, Queries: dataQuery, View: view})
	}
	simple("tailscale", enums.ServiceTailscale, datasetView)
	simple("mediaserver", enums.ServiceMediaServer, datasetView)
	simple("arr_upcoming", enums.ServiceArr, arrView)
	simple("grocy", enums.ServiceGrocy, datasetView)
	simple("dwd", enums.ServiceDWD, datasetView)
	simple("github", enums.ServiceGitHub, datasetView)
	simple("speedtest", enums.ServiceSpeedtest, speedView)

	Register(WidgetType{Key: "energy", Decode: decodeEnergy, Template: "widgets/energy", Category: CategoryInsight,
		Service: enums.ServiceTibber, RefreshS: integrationTTL, View: energyView,
		Queries: func(c any) []Query {
			queries := dataQuery(nil)
			if c.(EnergyConfig).PowerEntity != "" {
				queries = append(queries, Query{Name: peerHass, Source: "data", Conn: ConnPeer, Service: enums.ServiceHomeAssistant})
			}
			return queries
		}})
}
