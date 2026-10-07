package widgets

import (
	"reflect"
	"testing"

	"andon/internal/enums"
)

// Hint lists in dialogs name a hint's rule by its title, not its raw ID
// ("scrutiny.disk_hot").
func TestDialogHintsNameRule(t *testing.T) {
	const rule = "scrutiny.disk_hot"
	listed := []DetailHint{{ID: 1, Rule: rule, Severity: enums.SeverityWarn, Title: "hot"}}
	body := hintsDetail(HintsConfig{}, map[string]any{DetailHintsSlot: listed}, ViewCtx{}).Body.(*DetailBody)
	rows, ok := body.Blocks[0].Data.([]LitRow)
	if !ok || len(rows) != 1 {
		t.Fatalf("blocks: %+v", body.Blocks)
	}
	if want := Txt("rule_name." + rule); !reflect.DeepEqual(rows[0].Meta, want) {
		t.Errorf("row meta = %v, want %v", rows[0].Meta, want)
	}

	noise := noiseDetail(NoiseConfig{}, map[string]any{NoiseSlot: NoiseData{Flaps: []Flap{{Rule: rule, Returns: 3}}}}, ViewCtx{}).Body.(*DetailBody)
	if noise.List == nil || noise.List.Sub == rule {
		t.Errorf("noise list shows the raw rule ID: %+v", noise.List)
	}
}
