package rules

// Backup rules across tools:
//
//	truenas.snapshot_failed   a periodic snapshot task ended in ERROR
//	backups.gap               Komodo stacks / TrueNAS apps whose name appears
//	                          in no backup item of any tool – a name heuristic

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	Register("truenas.snapshot_failed", truenasSvc, nil, on(snapshotFailed))

	Register("backups.gap", Cross, nil, backupGap)
}

func snapshotFailed(data *sources.TrueNASDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, s := range data.Snapshots {
		if !s.Enabled || s.State != "ERROR" {
			continue
		}
		found = append(found, svcFinding(truenasSvc, "truenas.snapshot_failed", "snap:"+s.Dataset, "truenas.snapshot_failed",
			enums.SeverityWarn, strings.TrimRight(data.URL, "/")+"/ui/data-protection", map[string]any{"dataset": s.Dataset}))
	}
	return found
}

func backupGap(_ any, cfg map[string]any, env Env) []Finding {
	items, hasTool := backupItems(env.Datasets)
	if !hasTool {
		return nil
	}
	var missing []string
	for _, name := range serviceNames(env) {
		if !coveredBy(name, items) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{Fingerprint: "gap", Severity: enums.SeverityInfo, Message: "backups.gap",
		Params: map[string]any{"count": len(missing), "names": shortList(missing)}, Sources: []string{"backups"}}}
}

// backupItems lists lower-case names of everything a backup tool covers;
// false when the space has no backup tool at all.
func backupItems(datasets map[string]any) ([]string, bool) {
	var items []string
	seen := false
	for _, raw := range datasets {
		tool, ok := raw.(sources.BackupSource)
		if !ok {
			continue
		}
		jobs := tool.BackupJobs()
		if _, nas := raw.(*sources.TrueNASDataset); nas && len(jobs) == 0 {
			continue // a NAS without snapshot tasks is no backup tool
		}
		seen = true
		for _, j := range jobs {
			items = append(items, strings.ToLower(j.Item))
		}
	}
	return items, seen
}

// serviceNames lists what should be backed up: stacks and apps.
func serviceNames(env Env) []string {
	var names []string
	if komodo, ok := env.Datasets[string(enums.ServiceKomodo)].(*sources.KomodoDataset); ok {
		for _, s := range komodo.Stacks {
			names = append(names, s.Name)
		}
	}
	if nas, ok := env.Datasets[string(enums.ServiceTrueNAS)].(*sources.TrueNASDataset); ok {
		for _, a := range nas.Apps {
			names = append(names, a.Name)
		}
	}
	return names
}

// coveredBy: "immich" is covered by "immich-db" or "tank/immich".
func coveredBy(name string, items []string) bool {
	name = strings.ToLower(name)
	for _, item := range items {
		if strings.Contains(item, name) {
			return true
		}
	}
	return false
}
