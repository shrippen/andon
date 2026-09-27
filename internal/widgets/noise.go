package widgets

// "hint_noise": new hints per day and the rules whose hints come and go
// on their own, each linked to its settings.

// NoiseSlot carries NoiseData for ExtraNoise.
const NoiseSlot = "noise"

// NoiseData is the hint traffic as the widgets service hands it over.
type NoiseData struct {
	Daily []int
	Flaps []Flap
}

// Flap is a rule whose hints came back on their own this often.
type Flap struct {
	Rule    string
	Returns int
}

// NoiseDays is the window of the "hint_noise" tile.
const NoiseDays = 14

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

func init() {
	Register(WidgetType{Key: "hint_noise", Decode: decodeEmpty, Template: "widgets/hint_noise", Category: CategoryInsight,
		RefreshS: 1800, View: noiseView, Extra: ExtraNoise})
}
