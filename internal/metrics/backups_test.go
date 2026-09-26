package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestBackupsBorgStates: a Borg client with a failed last run is listed
// as failed first, one with a fresh backup as ok, one without as unknown.
func TestBackupsBorgStates(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	borg := &sources.BorgDataset{Clients: []sources.BorgClient{
		{Name: "nas", LastBackup: now.Add(-3 * time.Hour)},
		{Name: "laptop"},
		{Name: "server", LastBackup: now.Add(-2 * time.Hour), LastFailed: true},
	}}
	rows := metrics.Backups(borg, nil, nil, now, 26*time.Hour)
	got := map[string]metrics.BackupState{}
	for _, r := range rows {
		got[r.Item] = r.State
	}
	if got["server"] != metrics.BackupFailed || got["nas"] != metrics.BackupOK || got["laptop"] != metrics.BackupUnknown || rows[0].Item != "server" {
		t.Fatalf("rows: %+v", rows)
	}
}
