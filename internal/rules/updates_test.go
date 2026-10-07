package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// TestImageUpdateRules: WUD's newer tags are one note; Watchtower's failed
// update is a warning; both belong to the update topic.
func TestImageUpdateRules(t *testing.T) {
	now := time.Now()
	if got := run(t, "wud.updates", sources.DemoWUD(now), todayEnv(nil)); len(got) != 1 || got[0].Params["count"] != 2 {
		t.Fatalf("wud: %+v", got)
	}
	if got := run(t, "watchtower.failed", sources.DemoWatchtower(now), todayEnv(nil)); len(got) != 1 || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("watchtower: %+v", got)
	}
	topic := rules.RulesOf(rules.TopicUpdates)
	for _, id := range []string{"wud.updates", "watchtower.failed", "cross.release_newer"} {
		found := false
		for _, r := range topic {
			found = found || r == id
		}
		if !found {
			t.Fatalf("%s not in the update topic", id)
		}
	}
}

// TestUpdateUnbacked: Watchtower updated containers while the newest
// backup is days old; with a fresh one it is fine.
func TestUpdateUnbacked(t *testing.T) {
	now := time.Now().UTC()
	env := todayEnv(nil)
	wt := &sources.WatchtowerDataset{Updated: 2}
	env.Datasets = map[string]any{"watchtower": wt, "borgbackup": &sources.BorgDataset{Clients: []sources.BorgClient{{Name: "nas", LastBackup: now.AddDate(0, 0, -3)}}}}
	if got := run(t, "cross.update_unbacked", nil, env); len(got) != 1 || got[0].Params["count"] != 2 {
		t.Fatalf("old backup: %+v", got)
	}
	env.Datasets["borgbackup"] = &sources.BorgDataset{Clients: []sources.BorgClient{{Name: "nas", LastBackup: now.Add(-time.Hour)}}}
	if got := run(t, "cross.update_unbacked", nil, env); len(got) != 0 {
		t.Fatalf("fresh backup: %+v", got)
	}
}

// TestReleaseNewer: the demo watches immich-app/immich, whose release is
// newer than the running immich-server v1.131.0 (not than v2.1).
func TestReleaseNewer(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"github": sources.DemoGitHub(now), "docker": sources.DemoDocker(), "gitea": sources.DemoGitea(now)}
	got := run(t, "cross.release_newer", nil, env)
	if len(got) != 1 || got[0].Params["where"] != "immich-server" || got[0].Params["release"] != "v1.132.3" {
		t.Fatalf("release: %+v", got)
	}
}

// TestBackupJobs: tools without rules of their own (Kopia, Duplicati …)
// get one hint per failed or old job; Borg keeps its own rules.
func TestBackupJobs(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"duplicati": sources.DemoDuplicati(now), "borgbackup": sources.DemoBorg(now)}
	got := run(t, "backups.job", nil, env)
	if len(got) != 1 || got[0].Params["item"] != "Musik-Archiv" || got[0].Message != "backups.job_failed" {
		t.Fatalf("jobs: %+v", got)
	}
}
