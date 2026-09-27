package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestKimaiWeekMarksToday: today stands out, later days are dimmed and
// not flagged as short (15 Sep 2026 is a Tuesday).
func TestKimaiWeekMarksToday(t *testing.T) {
	kind, _ := widgets.Get("kimai_week")
	cfg, _ := widgets.Decode("kimai_week", map[string]any{})
	data := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{{Begin: "2026-09-14", Minutes: 120}}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceKimai, nil))
	days := view["Days"].([]widgets.DayCol)
	if days[0].Tier != "yellow" || !days[1].Today || days[1].Later {
		t.Fatalf("monday/today: %+v %+v", days[0], days[1])
	}
	if !days[2].Later || days[2].Tier != "" {
		t.Fatalf("wednesday still to come: %+v", days[2])
	}
}
