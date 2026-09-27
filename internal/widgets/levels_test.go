package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/widgets"
)

// TestLevelBar: counts per level, highest first, widths summing to 100.
func TestLevelBar(t *testing.T) {
	sevs := []enums.Severity{enums.SeverityWarn, enums.SeverityCritical, enums.SeverityWarn, enums.SeverityInfo}
	bar := widgets.LevelBar(sevs)
	if len(bar) != 3 || bar[0].Severity != enums.SeverityCritical || bar[1].N != 2 || bar[1].W != 50 || bar[2].W != 25 {
		t.Fatalf("bar: %+v", bar)
	}
	if widgets.LevelBar(nil) != nil {
		t.Fatal("no hints, no bar")
	}
}
