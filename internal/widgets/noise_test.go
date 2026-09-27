package widgets_test

import (
	"testing"

	"andon/internal/widgets"
)

// TestNoiseView: the daily counts become a line and a total.
func TestNoiseView(t *testing.T) {
	kind, ok := widgets.Get("hint_noise")
	if !ok {
		t.Fatal("hint_noise not registered")
	}
	data := widgets.NoiseData{Daily: []int{1, 4, 2}, Flaps: []widgets.Flap{{Rule: "freshrss.stale_feed", Returns: 11}}}
	view := kind.View(nil, map[string]any{widgets.NoiseSlot: data}, ctxFor("", nil))
	if view["Total"] != 7 || view["Spark"] == nil || len(view["Flaps"].([]widgets.Flap)) != 1 {
		t.Fatalf("view: %+v", view)
	}
}
