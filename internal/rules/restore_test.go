package rules_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
)

// Every quarter each backup system asks for one restore test; the hint
// is acknowledged once tested and comes back next quarter.
func TestRestoreUntested(t *testing.T) {
	env := crossEnv("2026-11-05", map[string]any{"borgbackup": struct{}{}, "pgbackweb": struct{}{}, "kimai": struct{}{}})
	got := run(t, "backups.restore_untested", nil, env)
	if len(got) != 2 || got[0].Fingerprint != "restore:borgbackup:2026-Q4" || got[0].Due != "2026-12-31" {
		t.Fatalf("findings: %+v", got)
	}
	next := run(t, "backups.restore_untested", nil, crossEnv("2027-01-10", env.Datasets))
	if next[0].Fingerprint != "restore:borgbackup:2027-Q1" {
		t.Fatalf("next quarter: %+v", next)
	}
	if none := run(t, "backups.restore_untested", nil, crossEnv("2026-11-05", map[string]any{"kimai": struct{}{}})); len(none) != 0 {
		t.Fatalf("no backup system, no hint: %+v", none)
	}
}

// A restore test marked this quarter quiets its system; one from the
// quarter before does not.
func TestRestoreMarked(t *testing.T) {
	marks := &metrics.History{Events: []metrics.Event{
		{At: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Kind: metrics.EventRestore, Subject: "borgbackup"},
		{At: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC), Kind: metrics.EventRestore, Subject: "pgbackweb"},
	}}
	env := crossEnv("2026-11-05", map[string]any{"borgbackup": struct{}{}, "pgbackweb": struct{}{}, metrics.HistoryDataset: marks})
	got := run(t, "backups.restore_untested", nil, env)
	if len(got) != 1 || got[0].Fingerprint != "restore:pgbackweb:2026-Q4" {
		t.Fatalf("findings: %+v", got)
	}
}
