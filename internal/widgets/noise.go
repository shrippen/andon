package widgets

import "andon/internal/enums"

// "hint_noise": new hints per day and the rules whose hints come and go
// on their own, each linked to its settings.

// NoiseSlot carries NoiseData for ExtraNoise.
const NoiseSlot = "noise"

// NoiseData is the hint traffic as the widgets service hands it over.
type NoiseData struct {
	Daily []int
	Flaps []Flap
	Open  map[enums.Severity][]int // open hints at each day's end, per level
}

// Flap is a rule whose hints came back on their own this often.
type Flap struct {
	Rule    string
	Returns int
}

// NoiseDays is the window of the "hint_noise" tile.
const NoiseDays = 14

// NoiseConfig is the "hint_noise" widget's config.
type NoiseConfig struct{ Days int }

func decodeNoise(raw map[string]any) any {
	days := NoiseDays
	switch raw["days"] {
	case "30":
		days = 30
	case "90":
		days = 90
	}
	return NoiseConfig{Days: days}
}

// decodeHintTrend is decodeNoise with 30 days unless set.
func decodeHintTrend(raw map[string]any) any {
	if _, set := raw["days"]; !set {
		return NoiseConfig{Days: trendDays}
	}
	return decodeNoise(raw)
}

// trendDays is the default window of the "hint_trend" tile.
const trendDays = 30

// ExtraDays is how far back the widgets service loads the traffic.
func (c NoiseConfig) ExtraDays() int { return c.Days }

// DaysWanter is a config that sets how many days its extra reaches back.
type DaysWanter interface{ ExtraDays() int }

func noiseView(_ any, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results[NoiseSlot].(NoiseData)
	if !ok {
		return map[string]any{}
	}
	values := make([]float64, len(data.Daily))
	total := 0
	for i, n := range data.Daily {
		values[i] = float64(n)
		total += n
	}
	return map[string]any{"Spark": SparkOf(values), "Total": total, "Days": len(data.Daily), "Flaps": data.Flaps}
}

// TrendLine is one level's open hints over the days of "hint_trend".
type TrendLine struct {
	Level enums.Severity
	Now   int
	Spark *Spark
}

// hintTrendView: is the homelab getting calmer? One line per level,
// critical first, today's count emphasized.
func hintTrendView(_ any, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results[NoiseSlot].(NoiseData)
	if !ok {
		return map[string]any{}
	}
	var lines []TrendLine
	for _, level := range []enums.Severity{enums.SeverityCritical, enums.SeverityWarn, enums.SeverityInfo} {
		counts := data.Open[level]
		if len(counts) == 0 {
			continue
		}
		values := make([]float64, len(counts))
		for i, n := range counts {
			values[i] = float64(n)
		}
		lines = append(lines, TrendLine{Level: level, Now: counts[len(counts)-1], Spark: SparkOf(values)})
	}
	return map[string]any{"Lines": lines, "Days": len(data.Daily)}
}

func init() {
	Register(WidgetType{Key: "hint_trend", Decode: decodeHintTrend, Template: "widgets/hint_trend", Category: CategoryInsight,
		RefreshS: 1800, View: hintTrendView, Extra: ExtraNoise})
	Register(WidgetType{Key: "hint_noise", Decode: decodeNoise, Template: "widgets/hint_noise", Category: CategoryInsight,
		RefreshS: 1800, View: noiseView, Extra: ExtraNoise})
}
