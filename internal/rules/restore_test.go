package rules_test

import (
	"testing"
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
