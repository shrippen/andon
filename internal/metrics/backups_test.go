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
	rows := metrics.Backups([]sources.BackupSource{borg}, now, 26*time.Hour)
	got := map[string]metrics.BackupState{}
	for _, r := range rows {
		got[r.Item] = r.State
	}
	if got["server"] != metrics.BackupFailed || got["nas"] != metrics.BackupOK || got["laptop"] != metrics.BackupUnknown || rows[0].Item != "server" {
		t.Fatalf("rows: %+v", rows)
	}
}

// fakeTool is a backup tool Backups knows only through the interface.
type fakeTool struct{ jobs []sources.BackupJob }

func (f fakeTool) BackupTool() string              { return "fake" }
func (f fakeTool) BackupJobs() []sources.BackupJob { return f.jobs }

// TestBackupsAnyTool: any BackupSource joins the overview and the last
// backup; a failed run counts as no backup.
func TestBackupsAnyTool(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	tool := fakeTool{jobs: []sources.BackupJob{{Item: "vm-101", Last: now.Add(-48 * time.Hour)}, {Item: "vm-102", Last: now.Add(-time.Hour), Failed: true}}}
	rows := metrics.Backups([]sources.BackupSource{tool}, now, 26*time.Hour)
	if len(rows) != 2 || rows[0].Item != "vm-102" || rows[0].State != metrics.BackupFailed || rows[1].State != metrics.BackupOld {
		t.Fatalf("rows %+v", rows)
	}
	last, name := metrics.LastBackup(map[string]any{"fake": tool})
	if !last.Equal(now.Add(-48*time.Hour)) || name.(map[string]any)["$t"] != "service.fake" {
		t.Fatalf("last %v %v", last, name)
	}
}
