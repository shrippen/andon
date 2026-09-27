package widgets

// "travel": distance from Dawarich's statistics (this month against the
// last, the year, countries and cities), plus the round trips to clients
// of the month (area mapping of the connection) and what they are worth
// at a mileage rate.

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// TravelConfig is the "travel" widget's config; KMRate 0 hides the amount.
type TravelConfig struct{ KMRate float64 }

// defaultKMRate is the German flat rate for business trips by car (€/km).
const defaultKMRate = 0.30

func decodeTravel(raw map[string]any) any {
	rate := defaultKMRate
	if v, ok := raw["km_rate"]; ok {
		rate = max(asFloat(v), 0)
	}
	return TravelConfig{KMRate: rate}
}

// yearStats finds one year in Dawarich's yearlyStats.
func yearStats(stats map[string]any, year int) map[string]any {
	list, _ := stats["yearlyStats"].([]any)
	for _, raw := range list {
		y, _ := raw.(map[string]any)
		if int(asFloat(y["year"])) == year {
			return y
		}
	}
	return nil
}

// monthKM reads one month's distance ("september") from a year's stats.
func monthKM(stats map[string]any, day time.Time) float64 {
	months, _ := yearStats(stats, day.Year())["monthlyDistanceKm"].(map[string]any)
	return asFloat(months[strings.ToLower(day.Month().String())])
}

func travelView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(TravelConfig)
	data, ok := results["data"].(*sources.DawarichDataset)
	if !ok {
		return map[string]any{}
	}
	today := parseToday(ctx.Today)
	year := yearStats(data.Stats, today.Year())
	out := map[string]any{"MonthKM": monthKM(data.Stats, today), "PrevKM": monthKM(data.Stats, metrics.AddMonths(today, -1)),
		"YearKM": asFloat(year["totalDistanceKm"]), "Countries": int(asFloat(year["totalCountriesVisited"])),
		"Cities": int(asFloat(year["totalCitiesVisited"])), "Rate": cfg.KMRate}

	trips := metrics.Trips(data, metrics.ParseAreaMapping(ctx.Options), metrics.MonthStart(today), today)
	km := 0.0
	for _, t := range trips {
		km += t.KM
	}
	out["Trips"], out["TripKM"], out["TripAmount"] = len(trips), km, km*cfg.KMRate
	if prev := out["PrevKM"].(float64); prev > 0 {
		out["MonthBar"] = min(pctOf(out["MonthKM"].(float64), prev), pctFull)
	}
	return out
}

func init() {
	Register(WidgetType{Key: "travel", Decode: decodeTravel, Template: "widgets/travel", Category: CategoryInsight,
		Service: enums.ServiceDawarich, RefreshS: 3600, Queries: dataQuery, View: travelView})
}
