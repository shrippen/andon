package widgets

import (
	"testing"

	"andon/internal/enums"
)

// TestHintsDetailCounts: the dialog counts every hint of the tile's
// filter, as the tile does, not only the ones it lists.
func TestHintsDetailCounts(t *testing.T) {
	listed := []DetailHint{{Severity: enums.SeverityCritical}, {Severity: enums.SeverityWarn}}
	all := []enums.Severity{enums.SeverityCritical, enums.SeverityCritical, enums.SeverityWarn, enums.SeverityInfo, enums.SeverityInfo}
	results := map[string]any{DetailHintsSlot: listed, TileViewSlot: map[string]any{"Severities": all}}
	facts := hintsDetail(HintsConfig{}, results, ViewCtx{}).Body.(*DetailBody).Facts
	if facts[0].Value != 5 || facts[1].Value != 2 || facts[2].Value != 1 {
		t.Fatalf("facts: %+v", facts)
	}
}
