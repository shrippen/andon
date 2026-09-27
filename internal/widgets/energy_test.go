package widgets_test

import (
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestEnergyStepCurve: prices are hourly steps (a horizontal run per
// hour), the cheapest window and now are placed on the same x scale.
func TestEnergyStepCurve(t *testing.T) {
	kind, _ := widgets.Get("energy")
	cfg, _ := widgets.Decode("energy", map[string]any{})
	start := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	data := &sources.TibberDataset{}
	for i, p := range []float64{0.30, 0.32, 0.25, 0.20, 0.21, 0.35} {
		data.Prices = append(data.Prices, sources.PricePoint{At: start.Add(time.Duration(i) * time.Hour), Total: p})
	}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceTibber, nil))
	path, _ := view["Path"].(string)
	if strings.Count(path, "H") != 6 {
		t.Fatalf("expected one horizontal run per hour: %s", path)
	}
	w := view["W"].(int)
	if x := view["NowX"].(float64); x <= 0 || x >= float64(w) {
		t.Fatalf("now off the curve: %v", x)
	}
	if x, cw := view["CheapX"].(float64), view["CheapW"].(float64); x <= 0 || cw <= 0 {
		t.Fatalf("cheap window: %v %v", x, cw)
	}
}
