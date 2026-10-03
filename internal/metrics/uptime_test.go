package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestUptimeDays: each run counts once per monitor; a day's share is up
// runs over all runs, days without runs are unknown (-1).
func TestUptimeDays(t *testing.T) {
	tally := metrics.Read(map[string]any{"kuma": &sources.KumaDataset{Monitors: []sources.KumaMonitor{
		{Name: "NAS", Status: sources.KumaDown}, {Name: "Shop", Status: sources.KumaUp}}}}, time.Now()).Counts
	if tally["kuma.runs.nas"] != 1 || tally["kuma.up.nas"] != 0 || tally["kuma.up.shop"] != 1 {
		t.Fatalf("tallies: %v", tally)
	}

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	day := func(back int) time.Time { return time.Date(2026, 9, 27-back, 0, 0, 0, 0, time.UTC) }
	h := &metrics.History{Series: map[string][]metrics.Point{
		"kuma.runs.nas": {{Day: day(2), Value: 4}, {Day: day(0), Value: 2}},
		"kuma.up.nas":   {{Day: day(2), Value: 3}, {Day: day(0), Value: 2}},
	}}
	days := metrics.UptimeDays(h, "NAS", now, 3)
	if len(days) != 3 || days[0] != 0.75 || days[1] != -1 || days[2] != 1 {
		t.Fatalf("days: %v", days)
	}
	if share, ok := metrics.Uptime(h, "NAS", day(2), now); !ok || share != 5.0/6 {
		t.Fatalf("uptime: %v %v", share, ok)
	}
}
