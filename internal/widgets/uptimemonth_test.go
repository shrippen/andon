package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestUptimeMonth: each monitor's share of up runs since the 1st, worst
// first; monitors without recorded runs are left out.
func TestUptimeMonth(t *testing.T) {
	kind, ok := widgets.Get("uptime_month")
	if !ok {
		t.Fatal("uptime_month not registered")
	}
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	h := &metrics.History{Series: map[string][]metrics.Point{
		"kuma.runs.nas":  {{Day: day(1), Value: 100}, {Day: day(15), Value: 100}},
		"kuma.up.nas":    {{Day: day(1), Value: 100}, {Day: day(15), Value: 80}},
		"kuma.runs.shop": {{Day: day(10), Value: 100}},
		"kuma.up.shop":   {{Day: day(10), Value: 100}},
	}}
	data := &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "Shop"}, {Name: "NAS"}, {Name: "Neu"}}}
	view := kind.View(nil, map[string]any{"data": data, widgets.HistorySlot: h}, ctxFor(enums.ServiceUptimeKuma, nil))
	rows := view["Rows"].([]widgets.UptimeRow)
	if len(rows) != 2 || rows[0].Name != "NAS" || rows[0].Share != 0.9 || rows[1].Share != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].DownHours <= 0 {
		t.Fatalf("downtime: %+v", rows[0])
	}
}

// TestUptimeMonthMeasuredSpan: downtime counts only the time Andon has
// measured; a monitor first seen today cannot be down for the whole month.
func TestUptimeMonthMeasuredSpan(t *testing.T) {
	kind, _ := widgets.Get("uptime_month")
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	h := &metrics.History{Series: map[string][]metrics.Point{
		"kuma.runs.nas": {{Day: today, Value: 10}}, "kuma.up.nas": {{Day: today, Value: 0}},
	}}
	data := &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "NAS"}}}
	view := kind.View(nil, map[string]any{"data": data, widgets.HistorySlot: h},
		widgets.ViewCtx{Today: today.Format(time.DateOnly), Service: string(enums.ServiceUptimeKuma)})
	rows := view["Rows"].([]widgets.UptimeRow)
	if len(rows) != 1 || rows[0].DownHours > 24 {
		t.Fatalf("downtime beyond what was measured: %+v", rows)
	}
}
