package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestExposureTile: public resources, the riskiest first, with how many
// go without login.
func TestExposureTile(t *testing.T) {
	kind, ok := widgets.Get("exposure")
	if !ok {
		t.Fatal("exposure not registered")
	}
	cfg, _ := widgets.Decode("exposure", map[string]any{})
	view := kind.View(cfg, map[string]any{"data": sources.DemoPangolin()}, ctxFor(enums.ServicePangolin, nil))
	rows, _ := view["Rows"].([]widgets.ExposedRow)
	if len(rows) == 0 {
		t.Fatalf("no rows: %+v", view)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Risk > rows[i-1].Risk {
			t.Fatalf("not riskiest first: %+v", rows)
		}
	}
	open := 0
	for _, r := range rows {
		if !r.Login {
			open++
		}
	}
	if view["Open"] != open {
		t.Fatalf("open count %v, want %d", view["Open"], open)
	}
}
