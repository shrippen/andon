package rules

// Image updates:
//
//	wud.updates             containers What's Up Docker found a newer tag for
//	watchtower.failed       Watchtower could not update containers
//	cross.update_unbacked   Watchtower updated containers, the newest backup is old
//	cross.release_newer     a watched repo's release is newer than a running image
//	backups.job             failed or old jobs of backup tools without rules of
//	                        their own (Kopia, Duplicati, Backrest, UrBackup, PBS)

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var (
	wudSvc        = string(enums.ServiceWUD)
	watchtowerSvc = string(enums.ServiceWatchtower)
)

// ownBackupRules are the tools whose jobs other rules cover already.
var ownBackupRules = map[string]bool{
	string(enums.ServiceBorgBackup): true, string(enums.ServicePGBackWeb): true, string(enums.ServiceTrueNAS): true,
}

func init() {
	Register("wud.updates", wudSvc, nil, on(wudUpdates))
	Register("watchtower.failed", watchtowerSvc, nil, on(watchtowerFailed))
	Register("cross.update_unbacked", Cross, map[string]any{"backup_hours": 26.0}, updateUnbacked)
	Register("cross.release_newer", Cross, nil, releaseNewer)
	Register("backups.job", Cross, map[string]any{"hours": 26.0}, backupJobs)
}

func wudUpdates(data *sources.WUDDataset, _ map[string]any, _ Env) []Finding {
	updates := data.Updates()
	if len(updates) == 0 {
		return nil
	}
	var names []string
	for _, u := range updates {
		names = append(names, u.Name+" "+u.Tag+" → "+u.NewTag)
	}
	return []Finding{svcFinding(wudSvc, "wud.updates", "updates", "wud.updates", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(updates), "names": shortList(names)})}
}

func watchtowerFailed(data *sources.WatchtowerDataset, _ map[string]any, _ Env) []Finding {
	if data.Failed == 0 {
		return nil
	}
	return []Finding{svcFinding(watchtowerSvc, "watchtower.failed", "failed", "watchtower.failed", enums.SeverityWarn, data.URL,
		map[string]any{"count": data.Failed, "scanned": data.Scanned})}
}

func updateUnbacked(_ any, cfg map[string]any, env Env) []Finding {
	wt, ok := env.Datasets[watchtowerSvc].(*sources.WatchtowerDataset)
	if !ok || wt.Updated == 0 {
		return nil
	}
	last, tool := metrics.LastBackup(env.Datasets)
	if !last.IsZero() && time.Since(last).Hours() < cfgFloat(cfg, "backup_hours") {
		return nil
	}
	params := map[string]any{"count": wt.Updated, "day": "–", "tool": "–"}
	if !last.IsZero() {
		params["day"], params["tool"] = Day(last), tool
	}
	return []Finding{{Fingerprint: "unbacked", Severity: enums.SeverityWarn, Message: "cross.update_unbacked", Params: params,
		Sources: []string{watchtowerSvc}}}
}

func releaseNewer(_ any, _ map[string]any, env Env) []Finding {
	gh, ok := env.Datasets[string(enums.ServiceGitHub)].(*sources.GitHubDataset)
	if !ok {
		return nil
	}
	var found []Finding
	for _, b := range metrics.ImagesBehind(metrics.RunningImages(env.Datasets), gh.Repos) {
		_, tag, _ := strings.Cut(b.Image.Image[strings.LastIndex(b.Image.Image, "/")+1:], ":")
		found = append(found, Finding{Fingerprint: b.Repo + "@" + b.Image.Where, Severity: enums.SeverityInfo, Message: "cross.release_newer",
			Params:    map[string]any{"where": b.Image.Where, "image": b.Image.Image, "tag": tag, "repo": b.Repo, "release": b.Release},
			ActionURL: "https://github.com/" + b.Repo + "/releases", ActionLabel: "open_in_github", Sources: []string{string(enums.ServiceGitHub)}})
	}
	return found
}

func backupJobs(_ any, cfg map[string]any, env Env) []Finding {
	maxAge := time.Duration(cfgFloat(cfg, "hours") * float64(time.Hour))
	var found []Finding
	for _, tool := range metrics.BackupTools(env.Datasets) {
		name := tool.BackupTool()
		if ownBackupRules[name] {
			continue
		}
		for _, row := range metrics.Backups([]sources.BackupSource{tool}, time.Now().UTC(), maxAge) {
			msg := map[metrics.BackupState]string{metrics.BackupFailed: "backups.job_failed", metrics.BackupOld: "backups.job_old"}[row.State]
			if msg == "" {
				continue
			}
			found = append(found, Finding{Fingerprint: name + ":" + row.Item, Severity: enums.SeverityWarn, Message: msg,
				Params:  map[string]any{"item": row.Item, "tool": map[string]any{"$t": "service." + name}, "day": agoParam(row.Last), "note": row.Note},
				Sources: []string{name}})
		}
	}
	return found
}
