package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestHeartbeatDown: a check that is down is a warning; late (grace),
// paused and new ones are not.
func TestHeartbeatDown(t *testing.T) {
	data := &sources.HealthchecksDataset{URL: "https://hc.example", Checks: []sources.Heartbeat{
		{Name: "a", Status: sources.HeartbeatDown}, {Name: "b", Status: sources.HeartbeatGrace},
		{Name: "c", Status: sources.HeartbeatPaused}, {Name: "d", Status: sources.HeartbeatNew}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"healthchecks": data}
	got := run(t, "heartbeat.down", nil, env)
	if len(got) != 1 || got[0].Params["name"] != "a" || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("down: %+v", got)
	}
}

// otherBeats is a second heartbeat tool, e.g. a cron runner.
type otherBeats []sources.Heartbeat

func (o otherBeats) Heartbeats() []sources.Heartbeat { return o }

// TestHeartbeatSources: every heartbeat tool of the space takes part,
// not only Healthchecks; the reason picks the hint text and each job
// links to its own page.
func TestHeartbeatSources(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"healthchecks": &sources.HealthchecksDataset{Checks: []sources.Heartbeat{
			{Name: "a", Status: sources.HeartbeatDown, Reason: sources.HeartbeatMissed, Tool: "healthchecks", Link: "https://hc.example/a"}}},
		"cronrunner": otherBeats{
			{Name: "b", Status: sources.HeartbeatDown, Reason: sources.HeartbeatFailed, Summary: "exit 2", Tool: "cronrunner", Link: "https://cr.example/b"}},
	}
	got := run(t, "heartbeat.down", nil, env)
	if len(got) != 2 {
		t.Fatalf("down: %+v", got)
	}
	b, a := got[0], got[1] // ordered by service key
	if a.Message != "heartbeat.down" || a.ActionURL != "https://hc.example/a" || a.Sources[0] != "healthchecks" {
		t.Fatalf("missed: %+v", a)
	}
	if b.Message != "heartbeat.failed" || b.Params["summary"] != "exit 2" || b.ActionURL != "https://cr.example/b" || b.Sources[0] != "cronrunner" {
		t.Fatalf("failed: %+v", b)
	}
}

// TestHeartbeatBackupOtherTool: a job of another tool is compared with
// its backup like a Healthchecks check.
func TestHeartbeatBackupOtherTool(t *testing.T) {
	now := time.Now().UTC()
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"cronrunner": otherBeats{{Name: "borg-nas", Status: sources.HeartbeatDown, Tool: "cronrunner"}},
		"borgbackup": &sources.BorgDataset{Clients: []sources.BorgClient{{Name: "nas", LastBackup: now.Add(-time.Hour)}}},
	}
	got := run(t, "cross.heartbeat_backup", nil, env)
	if len(got) != 1 || got[0].Message != "cross.heartbeat_down_backup_ok" || got[0].Sources[0] != "cronrunner" {
		t.Fatalf("other tool: %+v", got)
	}
}

// TestHeartbeatBackup: the demo's Invoice Ninja dump pings although its
// backup failed (warning), the laptop's Borg check is down while… its
// backup is old too (no finding: both agree); a fresh backup with a down
// check is a note.
func TestHeartbeatBackup(t *testing.T) {
	now := time.Now().UTC()
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"healthchecks": sources.DemoHealthchecks(now),
		"borgbackup":   sources.DemoBorg(now),
		"pgbackweb":    sources.DemoPGBack(now),
	}
	got := run(t, "cross.heartbeat_backup", nil, env)
	if len(got) != 1 || got[0].Params["check"] != "pgback-invoiceninja" || got[0].Message != "cross.heartbeat_ok_backup_not" {
		t.Fatalf("demo: %+v", got)
	}

	env.Datasets = map[string]any{
		"healthchecks": &sources.HealthchecksDataset{Checks: []sources.Heartbeat{{Name: "borg-nas", Status: sources.HeartbeatDown}}},
		"borgbackup":   &sources.BorgDataset{Clients: []sources.BorgClient{{Name: "nas", LastBackup: now.Add(-time.Hour)}}},
	}
	got = run(t, "cross.heartbeat_backup", nil, env)
	if len(got) != 1 || got[0].Message != "cross.heartbeat_down_backup_ok" || got[0].Severity != enums.SeverityInfo {
		t.Fatalf("fresh backup, silent check: %+v", got)
	}

	// Without a backup tool there is nothing to compare.
	env.Datasets = map[string]any{"healthchecks": sources.DemoHealthchecks(now)}
	if got := run(t, "cross.heartbeat_backup", nil, env); len(got) != 0 {
		t.Fatalf("no tool: %+v", got)
	}
}
