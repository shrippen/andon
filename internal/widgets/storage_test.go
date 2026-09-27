package widgets_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/widgets"
)

// TestStorageRowsProject: a pool filling 1 % a day shows how far it gets
// in 30 days and the day it is full, red when that is within a month.
func TestStorageRowsProject(t *testing.T) {
	kind, _ := widgets.Get("storage_forecast")
	today := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	var points []metrics.Point
	for i := 20; i >= 0; i-- {
		points = append(points, metrics.Point{Day: today.AddDate(0, 0, -i), Value: 0.8 - float64(i)*0.01})
	}
	h := &metrics.History{Series: map[string][]metrics.Point{"truenas.pool.tank.used": points}}
	view := kind.View(nil, map[string]any{widgets.HistorySlot: h}, ctxFor("", nil))
	rows := view["Rows"].([]widgets.StorageRow)
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[0]
	if r.FullIn < 19 || r.FullIn > 20 || r.FullOn < "2026-10-04" || r.Tier != "red" || r.Ahead < 19.9 || r.Ahead > 20.1 {
		t.Fatalf("row: %+v", r)
	}
}
