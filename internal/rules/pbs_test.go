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

// TestPBSPool: a store on a TrueNAS pool (PBS option pools). The pool is
// near full while the store still has room: warn. The pool is much
// fuller than the store says: info. Stores without a pool, or a pool
// TrueNAS does not know, say nothing.
func TestPBSPool(t *testing.T) {
	const tb = 1e12
	pbs := &sources.PBSDataset{Stores: []sources.PBSStore{
		{Store: "archiv", Pool: "tank", Total: 3 * tb, Used: 1.2 * tb}, // 40 %, pool 88 %
		{Store: "fast", Pool: "ssd", Total: 1 * tb, Used: 0.2 * tb},    // 20 %, pool 50 %
		{Store: "same", Pool: "ssd", Total: 1 * tb, Used: 0.45 * tb},   // 45 %, pool 50 %
		{Store: "local", Total: 1 * tb, Used: 0.1 * tb},                // no pool
		{Store: "lost", Pool: "gone", Total: 1 * tb, Used: 0.1 * tb},   // unknown pool
	}}
	nas := &sources.TrueNASDataset{Pools: []sources.Pool{{Name: "Tank", Size: 16 * tb, Allocated: 14.08 * tb}, {Name: "ssd", Size: 2 * tb, Allocated: 1 * tb}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"pbs": pbs, "truenas": nas}

	got := run(t, "cross.pbs_pool", nil, env)
	if len(got) != 2 {
		t.Fatalf("found %+v", got)
	}
	if got[0].Message != "cross.pbs_pool_full" || got[0].Params["store"] != "archiv" || got[0].Params["pool"] != "Tank" {
		t.Fatalf("full %+v", got[0])
	}
	if got[1].Message != "cross.pbs_pool_gap" || got[1].Params["store"] != "fast" {
		t.Fatalf("gap %+v", got[1])
	}
}

// TestPBSPoolDemo: the demo's archive store sits on tank, which is 88 %
// full while the store is at 39 %.
func TestPBSPoolDemo(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{"pbs": sources.DemoPBS(time.Now()), "truenas": sources.DemoTrueNAS()}
	got := run(t, "cross.pbs_pool", nil, env)
	if len(got) != 1 || got[0].Params["store"] != "archiv" || got[0].Message != "cross.pbs_pool_full" {
		t.Fatalf("demo %+v", got)
	}
}
