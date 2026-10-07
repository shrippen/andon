package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestPBSRules: the demo's last verify failed, its store is 92 % full.
func TestPBSRules(t *testing.T) {
	pbs := sources.DemoPBS(time.Now())
	if got := run(t, "pbs.verify_failed", pbs, todayEnv(nil)); len(got) != 1 || got[0].Params["store"] != "nebelhorn" {
		t.Fatalf("verify: %+v", got)
	}
	if got := run(t, "pbs.datastore_full", pbs, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("full: %+v", got)
	}
}

// TestProxmoxBackupCountsPBS: a guest that Proxmox lists without a fresh
// backup is fine when PBS has a fresh one; PBS groups of guests Proxmox no
// longer has are orphans.
func TestProxmoxBackupCountsPBS(t *testing.T) {
	now := time.Now().UTC()
	pve := &sources.ProxmoxDataset{Guests: []sources.ProxmoxGuest{{VMID: 101, Name: "ha"}, {VMID: 102, Name: "web"}},
		Backups: map[int64]time.Time{101: now.AddDate(0, 0, -9), 102: now.AddDate(0, 0, -9)}}
	pbs := &sources.PBSDataset{Groups: []sources.PBSGroup{{Store: "s", Type: "vm", ID: "101", Last: now.Add(-time.Hour)},
		{Store: "s", Type: "ct", ID: "105", Last: now.AddDate(0, 0, -40)}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"proxmox": pve, "pbs": pbs}

	old := run(t, "proxmox.backup_old", pve, env)
	if len(old) != 1 || old[0].Params["vmid"] != "102" {
		t.Fatalf("backup_old: %+v", old)
	}
	orphans := run(t, "cross.pbs_orphan", nil, env)
	if len(orphans) != 1 || orphans[0].Params["count"] != 1 || orphans[0].Params["ids"] != "ct/105" {
		t.Fatalf("orphans: %+v", orphans)
	}
}
