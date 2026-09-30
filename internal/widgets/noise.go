package widgets

import (
	"strconv"

	"andon/internal/enums"
)

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

// decodeNoise reads the days select: "30" → 30.
func decodeNoise(r Raw) NoiseConfig {
	days, _ := strconv.Atoi(r.Pick("days"))
	return NoiseConfig{Days: days}
}

// ExtraDays is how far back the widgets service loads the traffic.
func (c NoiseConfig) ExtraDays() int { return c.Days }

// DaysWanter is a config that sets how many days its extra reaches back.
type DaysWanter interface{ ExtraDays() int }

func noiseView(_ NoiseConfig, results map[string]any, _ ViewCtx) map[string]any {
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
func hintTrendView(_ NoiseConfig, results map[string]any, _ ViewCtx) map[string]any {
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
	Tile[NoiseConfig]{Key: "hint_trend", Category: CategoryInsight, Topic: TopicOverview, RefreshS: 1800, Extra: ExtraNoise,
		Fields:  []Field{sel("days", "30", "14", "30", "90")},
		Renames: []rename{{from: "period", to: "days"}},
		Decode:  decodeNoise, View: hintTrendView}.add()
	Tile[NoiseConfig]{Key: "hint_noise", Category: CategoryInsight, Topic: TopicOverview, RefreshS: 1800, Extra: ExtraNoise,
		Fields:  []Field{sel("days", "14", "14", "30", "90")},
		Renames: []rename{{from: "period", to: "days"}},
		Decode:  decodeNoise, View: noiseView}.add()
}
