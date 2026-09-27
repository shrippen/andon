package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/widgets"
)

// TestStatusLight: the light follows the most urgent open hint by the
// tile's own thresholds; only what is not green is listed.
func TestStatusLight(t *testing.T) {
	kind, ok := widgets.Get("status_light")
	if !ok {
		t.Fatal("status_light not registered")
	}
	briefs := []widgets.HintBrief{
		{ID: 1, Severity: enums.SeverityWarn, Title: "Backup alt"},
		{ID: 2, Severity: enums.SeverityInfo, Title: "Feed still"},
	}
	light := func(raw map[string]any) map[string]any {
		cfg, _ := widgets.Decode("status_light", raw)
		return kind.View(cfg, map[string]any{widgets.HintsSlot: briefs}, ctxFor("", nil))
	}
	if v := light(map[string]any{}); v["State"] != "yellow" || len(v["Items"].([]widgets.HintBrief)) != 1 {
		t.Fatalf("default (red critical, yellow warning): %+v", v)
	}
	if v := light(map[string]any{"red_from": "warn"}); v["State"] != "red" {
		t.Fatalf("red from warning: %+v", v)
	}
	if v := light(map[string]any{"yellow_from": "off"}); v["State"] != "green" || len(v["Items"].([]widgets.HintBrief)) != 0 {
		t.Fatalf("yellow off: %+v", v)
	}
	cfg, _ := widgets.Decode("status_light", map[string]any{"yellow_from": "info"})
	if src, ok := cfg.(widgets.HintSource); !ok || src.Hints().MinSeverity != int(enums.SeverityInfo) {
		t.Fatalf("hint filter: %+v", cfg)
	}
}
