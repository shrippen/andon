package widgets

// "travel": distance from Dawarich's statistics (this month against the
// last, the year, countries and cities), plus the round trips to clients
// of the month (area mapping of the connection) and what they are worth
// at a mileage rate.

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// TravelConfig is the "travel" widget's config; KMRate 0 hides the amount.
type TravelConfig struct {
	KMRate  float64
	Year    bool // the year against last year instead of the month
	HideBar bool // no bar against the previous period
}

// defaultKMRate is the German flat rate for business trips by car (€/km).
const defaultKMRate = 0.30

func travelView(cfg TravelConfig, data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) map[string]any {
	today := todayOf(ctx)
	year := metrics.DawarichYear(data.Stats, today.Year())
	out := map[string]any{"MonthKM": metrics.DawarichMonthKM(data.Stats, today), "PrevKM": metrics.DawarichMonthKM(data.Stats, metrics.AddMonths(today, -1)),
		"YearKM": asFloat(year["totalDistanceKm"]), "Countries": int(asFloat(year["totalCountriesVisited"])),
		"Cities": int(asFloat(year["totalCitiesVisited"])), "Rate": cfg.KMRate}

	trips := metrics.Trips(data, travelAreas(data, ctx, results), metrics.MonthStart(today), today)
	km := 0.0
	for _, t := range trips {
		km += t.KM
	}
	out["Trips"], out["TripKM"], out["TripAmount"] = len(trips), km, km*cfg.KMRate

	// The head: this month against the last, or this year against the last.
	head, prev := out["MonthKM"].(float64), out["PrevKM"].(float64)
	if cfg.Year {
		head, prev = out["YearKM"].(float64), asFloat(metrics.DawarichYear(data.Stats, today.Year()-1)["totalDistanceKm"])
		out["PrevKM"] = prev
	}
	out["HeadKM"], out["Year"] = head, cfg.Year
	if prev > 0 && !cfg.HideBar {
		out["Bar"] = min(pctOf(head, prev), pctFull)
	}
	return out
}

func init() {
	Tile[TravelConfig]{Key: "travel", Detail: dataDetail(travelDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceDawarich, RefreshS: 3600,
		Fields: []Field{{Key: "km_rate", Input: InputNumber, Default: defaultKMRate, Min: "0"}, sel("period", "month", "month", "year"),
			{Key: "hide_bar", Input: InputCheck}},
		Decode: func(r Raw) TravelConfig {
			return TravelConfig{KMRate: r.Float("km_rate"), Year: r.Pick("period") == "year", HideBar: r.Bool("hide_bar")}
		},
		Queries: func(TravelConfig) []Query { return append(dataQuery(nil), kimaiPeer) }, View: travelTile}.add()
}

// travelTile is travelView on the tile's own Dawarich data.
func travelTile(cfg TravelConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results[dataName].(*sources.DawarichDataset)
	if !ok {
		return map[string]any{}
	}
	return travelView(cfg, data, ctx, results)
}

// travelAreas is the area mapping with the places of the Kimai peer.
func travelAreas(data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) map[string]metrics.AreaMapping {
	kimai, _ := results[peerKimai].(*sources.KimaiDataset)
	return metrics.AreaMap(data, kimai, ctx.Options)
}
