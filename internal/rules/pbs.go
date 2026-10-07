package rules

// Proxmox Backup Server:
//
//	pbs.verify_failed    a store's newest verify run found errors
//	pbs.datastore_full   a store is fuller than "percent"
//	cross.pbs_orphan     backups of VMs or containers Proxmox no longer has
//	                     (they only take space)
//	cross.pbs_pool       a store's TrueNAS pool (PBS option pools) is near
//	                     full while the store has room, or far fuller than
//	                     the store says (other datasets share the pool)
//
// proxmox.backup_old (ops.go) counts a fresh PBS backup of the guest too.

import (
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var pbsSvc = string(enums.ServicePBS)

// pbsGuestTypes are the group types that belong to Proxmox guests.
var pbsGuestTypes = map[string]bool{"vm": true, "ct": true}

func init() {
	Register("pbs.verify_failed", pbsSvc, nil, on(pbsVerifyFailed))
	Register("pbs.datastore_full", pbsSvc, map[string]any{"percent": 90.0}, on(pbsFull))
	Register("cross.pbs_orphan", Cross, nil, pbsOrphan)
	Register("cross.pbs_pool", Cross, map[string]any{"percent": 85.0, "gap": 15.0}, pbsPool)
}

func pbsPool(_ any, cfg map[string]any, env Env) []Finding {
	pbs, _ := env.Datasets[pbsSvc].(*sources.PBSDataset)
	nas, _ := env.Datasets[string(enums.ServiceTrueNAS)].(*sources.TrueNASDataset)
	limit, gap := cfgFloat(cfg, "percent"), cfgFloat(cfg, "gap")

	var found []Finding
	for _, f := range metrics.PBSPools(pbs, nas) {
		msg, sev := "", enums.SeverityInfo
		switch {
		case f.PoolPct >= limit && f.StorePct < limit:
			msg, sev = "cross.pbs_pool_full", enums.SeverityWarn
		case f.PoolPct-f.StorePct >= gap:
			msg = "cross.pbs_pool_gap"
		default:
			continue
		}
		found = append(found, Finding{Fingerprint: "pool:" + f.Store, Severity: sev, Message: msg,
			Params:  map[string]any{"store": f.Store, "pool": f.Pool, "store_pct": Num(f.StorePct, 0), "pool_pct": Num(f.PoolPct, 0)},
			Sources: []string{pbsSvc, string(enums.ServiceTrueNAS)}})
	}
	return found
}

func pbsVerifyFailed(data *sources.PBSDataset, _ map[string]any, _ Env) []Finding {
	seen := map[string]bool{}
	var found []Finding
	for _, v := range data.Verifies {
		if seen[v.Store] {
			continue
		}
		seen[v.Store] = true
		if v.OK() {
			continue
		}
		found = append(found, svcFinding(pbsSvc, "pbs.verify_failed", "verify:"+v.Store, "pbs.verify_failed", enums.SeverityWarn,
			data.URL, map[string]any{"store": v.Store, "day": Day(v.At), "status": v.Status}))
	}
	return found
}

func pbsFull(data *sources.PBSDataset, cfg map[string]any, _ Env) []Finding {
	var found []Finding
	for _, s := range data.Stores {
		if s.Total <= 0 {
			continue
		}
		pct := s.Used / s.Total * 100
		if pct < cfgFloat(cfg, "percent") {
			continue
		}
		found = append(found, svcFinding(pbsSvc, "pbs.datastore_full", "full:"+s.Store, "pbs.datastore_full", enums.SeverityWarn,
			data.URL, map[string]any{"store": s.Store, "percent": Num(pct, 0)}))
	}
	return found
}

// pbsLast is the newest PBS backup of a Proxmox guest, zero if none.
func pbsLast(env Env, vmid string) time.Time {
	pbs, ok := env.Datasets[pbsSvc].(*sources.PBSDataset)
	if !ok {
		return time.Time{}
	}
	var last time.Time
	for _, g := range pbs.Groups {
		if pbsGuestTypes[g.Type] && g.ID == vmid && g.Last.After(last) {
			last = g.Last
		}
	}
	return last
}

func pbsOrphan(_ any, _ map[string]any, env Env) []Finding {
	pbs, ok := env.Datasets[pbsSvc].(*sources.PBSDataset)
	pve, ok2 := env.Datasets[string(enums.ServiceProxmox)].(*sources.ProxmoxDataset)
	if !ok || !ok2 {
		return nil
	}
	guests := map[string]bool{}
	for _, g := range pve.Guests {
		guests[strconv.FormatInt(g.VMID, 10)] = true
	}
	var orphans []string
	for _, g := range pbs.Groups {
		if pbsGuestTypes[g.Type] && !guests[g.ID] {
			orphans = append(orphans, g.Type+"/"+g.ID)
		}
	}
	if len(orphans) == 0 {
		return nil
	}
	return []Finding{{Fingerprint: "orphans", Severity: enums.SeverityInfo, Message: "cross.pbs_orphan",
		Params: map[string]any{"count": len(orphans), "ids": strings.Join(orphans, ", ")}, Sources: []string{pbsSvc, string(enums.ServiceProxmox)}}}
}
