package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestBackupDays: each run marks whether an item's newest backup falls on
// today; the widget reads the marks back day by day.
func TestBackupDays(t *testing.T) {
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC)
	marks := metrics.Read(metrics.Scope{Datasets: map[string]any{"borg": &sources.BorgDataset{Clients: []sources.BorgClient{
		{Name: "nas", LastBackup: now.Add(-3 * time.Hour)}, {Name: "shop-db", LastBackup: now.AddDate(0, 0, -3)}}}}}, now).Values
	if marks["backup.borgbackup.nas"] != 1 || marks["backup.borgbackup.shop-db"] != 0 {
		t.Fatalf("marks: %v", marks)
	}

	day := func(back int) time.Time { return time.Date(2026, 9, 27-back, 0, 0, 0, 0, time.UTC) }
	h := &metrics.History{Series: map[string][]metrics.Point{
		"backup.borgbackup.nas": {{Day: day(2), Value: 1}, {Day: day(1), Value: 0}, {Day: day(0), Value: 1}},
	}}
	got := metrics.BackupDays(h, "borgbackup", "nas", now, 4)
	if len(got) != 4 || got[0] != -1 || got[1] != 1 || got[2] != 0 || got[3] != 1 {
		t.Fatalf("days: %v", got)
	}
}
