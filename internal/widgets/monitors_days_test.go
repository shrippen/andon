package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestMonitorsShowDays: with recorded runs each monitor gets a line of
// 14 days (down first), coloured by the day's uptime.
func TestMonitorsShowDays(t *testing.T) {
	kind, _ := widgets.Get("monitors")
	today := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	h := &metrics.History{Series: map[string][]metrics.Point{
		"kuma.runs.nas": {{Day: today, Value: 10}}, "kuma.up.nas": {{Day: today, Value: 5}},
		"kuma.runs.shop": {{Day: today, Value: 10}}, "kuma.up.shop": {{Day: today, Value: 10}},
	}}
	data := &sources.KumaDataset{Monitors: []sources.KumaMonitor{
		{Name: "Shop", Status: sources.KumaUp, MS: 210}, {Name: "NAS", Status: sources.KumaDown}}}
	view := kind.View(nil, map[string]any{"data": data, widgets.HistorySlot: h}, ctxFor(enums.ServiceUptimeKuma, nil))

	lines, ok := view["Lines"].([]widgets.MonitorLine)
	if !ok || len(lines) != 2 || lines[0].Name != "NAS" || len(lines[0].Days) != 14 {
		t.Fatalf("lines: %+v", view["Lines"])
	}
	if got := lines[0].Days[13].State; got != "bad" {
		t.Fatalf("NAS today: %s", got)
	}
	if got := lines[1].Days[13].State; got != "ok" {
		t.Fatalf("Shop today: %s", got)
	}
	if got := lines[1].Days[0].State; got != "none" {
		t.Fatalf("day without runs: %s", got)
	}
}
