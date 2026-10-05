package widgets

// "travel": the rides of this month (or year) as Dawarich tracked them,
// business against private (metrics.Classify), against the period before;
// Dawarich's stats add countries and cities. The rides need the Kimai
// peer for booked time, the mileage plugin's places and trips.

import (
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// TravelConfig is the "travel" widget's config; KMRate 0 takes the
// space's rate.
type TravelConfig struct {
	KMRate  float64
	Year    bool // the year against last year instead of the month
	HideBar bool // no bar of the business share
}

// travelOf classifies the rides of the tile's Dawarich data.
func travelOf(data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) metrics.Travel {
	kimai, _ := results[peerKimai].(*sources.KimaiDataset)
	return metrics.TravelOf(data, kimai, ctx.Options, metrics.TravelSettingsOf(ctx.Settings), time.Now())
}

// travelRate is the widget's km rate, else the space's.
func travelRate(cfg TravelConfig, ctx ViewCtx) float64 {
	if cfg.KMRate > 0 {
		return cfg.KMRate
	}
	return metrics.TravelSettingsOf(ctx.Settings).KMRate
}

// travelPeriod is the tile's period and the one before.
func travelPeriod(cfg TravelConfig, today time.Time) (start, prevStart, prevEnd time.Time) {
	if cfg.Year {
		start = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(-1, 0, 0), start.AddDate(0, 0, -1)
	}
	start = metrics.MonthStart(today)
	return start, start.AddDate(0, -1, 0), start.AddDate(0, 0, -1)
}

func travelView(cfg TravelConfig, data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) map[string]any {
	today := todayOf(ctx)
	travel := travelOf(data, ctx, results)
	start, prevStart, prevEnd := travelPeriod(cfg, today)
	now := metrics.ByClass(travel.Between(start, today))
	prev := metrics.ByClass(travel.Between(prevStart, prevEnd))

	year := metrics.DawarichYear(data.Stats, today.Year())
	out := map[string]any{"Year": cfg.Year, "Countries": int(asFloat(year["totalCountriesVisited"])), "Cities": int(asFloat(year["totalCitiesVisited"])),
		"Estimated": travel.Estimated, "Partial": travel.Partial, "Rate": travelRate(cfg, ctx)}

	var head, before float64
	for _, c := range metrics.RideClasses {
		head += now[c].KM
		before += prev[c].KM
	}
	business := now[metrics.ClassBusiness]
	out["HeadKM"], out["PrevKM"] = head, before
	out["BusinessKM"], out["CommuteKM"], out["PrivateKM"] = business.KM, now[metrics.ClassCommute].KM, now[metrics.ClassPrivate].KM
	out["Trips"], out["TripAmount"] = business.Rides, business.KM*travelRate(cfg, ctx)
	if head > 0 && !cfg.HideBar {
		out["Bar"] = min(pctOf(business.KM, head), pctFull)
	}
	return out
}

func init() {
	Tile[TravelConfig]{Key: "travel", Detail: dataDetail(travelDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceDawarich, RefreshS: 3600,
		Fields: []Field{{Key: "km_rate", Input: InputNumber, Min: "0"}, sel("period", "month", "month", "year"),
			{Key: "hide_bar", Input: InputCheck}},
		Decode: func(r Raw) TravelConfig {
			return TravelConfig{KMRate: r.Float("km_rate"), Year: r.Pick("period") == "year", HideBar: r.Bool("hide_bar")}
		},
		Queries:       func(TravelConfig) []Query { return append(dataQuery(nil), kimaiPeer) },
		DetailQueries: func(TravelConfig) []Query { return []Query{peer(peerSure, enums.ServiceSure)} }, View: travelTile}.add()
}

// travelTile is travelView on the tile's own Dawarich data.
func travelTile(cfg TravelConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results[dataName].(*sources.DawarichDataset)
	if !ok {
		return map[string]any{}
	}
	return travelView(cfg, data, ctx, results)
}
