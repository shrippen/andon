package rules

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

func init() {
	registerFreshRSS()
	registerGitea()
	registerBorg()
}

func registerFreshRSS() {
	// A reading backlog that only grows: name the feeds causing it, so
	// the fix (unsubscribe, filter, mark read) is obvious.
	Register("freshrss.backlog", freshrssSvc, map[string]any{"min_count": 500.0}, on(freshrssBacklog))

	// One hint for all silent feeds, longest silent first: one per feed
	// buried every other hint.
	Register("freshrss.stale_feed", freshrssSvc, map[string]any{"days": 180.0}, on(staleFeed))
}

func freshrssBacklog(data *sources.FreshRSSDataset, cfg map[string]any, env Env) []Finding {
	if data.Unread < cfgInt(cfg, "min_count") {
		return nil
	}
	feeds := append([]sources.Feed(nil), data.Feeds...)
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].Unread > feeds[j].Unread })
	var top []string
	for _, f := range feeds[:min(3, len(feeds))] {
		if f.Unread > 0 {
			top = append(top, fmt.Sprintf("%s (%d)", f.Title, f.Unread))
		}
	}
	return []Finding{svcFinding(freshrssSvc, "freshrss.backlog", "backlog", "freshrss.backlog", enums.SeverityInfo, data.URL,
		map[string]any{"count": data.Unread, "feeds": strings.Join(top, ", ")})}
}

func staleFeed(data *sources.FreshRSSDataset, cfg map[string]any, env Env) []Finding {
	var silent []aged
	for _, f := range data.Feeds {
		if !metrics.FeedSilent(f, env.Today, cfgFloat(cfg, "days")) {
			continue
		}
		silent = append(silent, aged{f.Title, int(env.Today.Sub(f.Newest).Hours() / hoursPerDay)})
	}
	if len(silent) == 0 {
		return nil
	}
	names := agedNames(silent)
	return []Finding{svcFinding(freshrssSvc, "freshrss.stale_feed", "stale", "freshrss.stale", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(silent), "days": cfgInt(cfg, "days"), "oldest": silent[0].days, "names": names})}
}

func registerGitea() {
	Register("gitea.review_waiting", giteaSvc, map[string]any{"days": 2.0}, on(reviewWaiting))

	Register("gitea.due", giteaSvc, map[string]any{"warn_days": 3.0}, on(giteaDue))

	Register("gitea.stale_pr", giteaSvc, map[string]any{"days": 14.0}, on(stalePr))

	Register("gitea.actions_failed", giteaSvc, nil, on(actionsFailed))

	Register("gitea.mirror_stale", giteaSvc, map[string]any{"days": 7.0}, on(mirrorStale))
}

// giteaFinding is a finding about one issue or pull request.
func giteaFinding(rule, msg string, level enums.Severity, i sources.Issue, params map[string]any) Finding {
	p := map[string]any{"repo": i.Repo, "number": i.Number, "title": i.Title}
	for k, v := range params {
		p[k] = v
	}
	f := svcFinding(giteaSvc, rule, fmt.Sprintf("%s#%d", i.Repo, i.Number), msg, level, i.URL, p)
	return f
}

func reviewWaiting(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, pr := range data.Reviews {
		days := int(env.Today.Sub(pr.Updated).Hours() / hoursPerDay)
		if days < cfgInt(cfg, "days") {
			continue
		}
		found = append(found, giteaFinding("gitea.review_waiting", "gitea.review", enums.SeverityWarn, pr, map[string]any{"days": days}))
	}
	return found
}

func giteaDue(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, i := range data.Assigned {
		if i.Due.IsZero() {
			continue
		}
		left := int(i.Due.Sub(env.Today).Hours() / hoursPerDay)
		level := enums.SeverityCritical
		if left > cfgInt(cfg, "warn_days") {
			continue
		} else if left >= 0 {
			level = enums.SeverityWarn
		}
		f := giteaFinding("gitea.due", "gitea.due", level, i, map[string]any{"day": Day(i.Due)})
		f.Due = i.Due.Format("2006-01-02")
		found = append(found, f)
	}
	return found
}

func stalePr(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, i := range data.Assigned {
		days := int(env.Today.Sub(i.Updated).Hours() / hoursPerDay)
		if !i.Pull || days < cfgInt(cfg, "days") {
			continue
		}
		found = append(found, giteaFinding("gitea.stale_pr", "gitea.stale_pr", enums.SeverityInfo, i, map[string]any{"days": days}))
	}
	return found
}

func actionsFailed(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, r := range data.Repos {
		if r.FailedWorkflow == "" {
			continue
		}
		found = append(found, svcFinding(giteaSvc, "gitea.actions_failed", "actions:"+r.Name, "gitea.actions", enums.SeverityWarn,
			strings.TrimRight(r.URL, "/")+"/actions", map[string]any{"repo": r.Name, "run": r.FailedWorkflow}))
	}
	return found
}

func mirrorStale(data *sources.GiteaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, r := range data.Repos {
		if !r.Mirror || r.MirrorUpdated.IsZero() || env.Today.Sub(r.MirrorUpdated).Hours()/hoursPerDay <= cfgFloat(cfg, "days") {
			continue
		}
		found = append(found, svcFinding(giteaSvc, "gitea.mirror_stale", "mirror:"+r.Name, "gitea.mirror", enums.SeverityWarn,
			strings.TrimRight(r.URL, "/")+"/settings", map[string]any{"repo": r.Name, "day": Day(r.MirrorUpdated)}))
	}
	return found
}

func registerBorg() {
	Register("borg.client_offline", borgSvc, map[string]any{"days": 2.0}, on(clientOffline))

	Register("borg.jobs_failed", borgSvc, nil, on(jobsFailed))

	Register("borg.backup_old", borgSvc, map[string]any{"warn_hours": 26.0, "critical_hours": 72.0}, on(backupOld))

	Register("borg.storage", borgSvc, map[string]any{"warn": 0.85, "critical": 0.95}, on(borgStorage))

	Register("borg.update", borgSvc, nil, on(borgUpdate))
}

func clientOffline(data *sources.BorgDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, c := range data.Clients {
		switch {
		case c.Status == "error":
			found = append(found, svcFinding(borgSvc, "borg.client_offline", "client:"+c.Name, "borg.client_error",
				enums.SeverityCritical, data.URL, map[string]any{"client": c.Name}))
		case c.Status == "offline" && !c.LastSeen.IsZero() && env.Today.Sub(c.LastSeen).Hours()/hoursPerDay > cfgFloat(cfg, "days"):
			found = append(found, svcFinding(borgSvc, "borg.client_offline", "client:"+c.Name, "borg.client_offline",
				enums.SeverityWarn, data.URL, map[string]any{"client": c.Name, "day": Day(c.LastSeen)}))
		}
	}
	return found
}

func jobsFailed(data *sources.BorgDataset, cfg map[string]any, env Env) []Finding {
	if data.Failed24h == 0 {
		return nil
	}
	return []Finding{svcFinding(borgSvc, "borg.jobs_failed", "failed", "borg.failed", enums.SeverityWarn,
		strings.TrimRight(data.URL, "/")+"/queue", map[string]any{"count": data.Failed24h, "ok": data.Completed24h})}
}

func backupOld(data *sources.BorgDataset, cfg map[string]any, env Env) []Finding {
	if data.LastBackup.IsZero() {
		return nil
	}
	hours := time.Now().UTC().Sub(data.LastBackup).Hours()
	level := enums.SeverityWarn
	if hours >= cfgFloat(cfg, "critical_hours") {
		level = enums.SeverityCritical
	} else if hours < cfgFloat(cfg, "warn_hours") {
		return nil
	}
	return []Finding{svcFinding(borgSvc, "borg.backup_old", "last", "borg.old", level, data.URL,
		map[string]any{"hours": int(hours)})}
}

func borgStorage(data *sources.BorgDataset, cfg map[string]any, env Env) []Finding {
	if data.TotalBytes == 0 {
		return nil
	}
	share := data.UsedBytes / data.TotalBytes
	level := enums.SeverityWarn
	if share >= cfgFloat(cfg, "critical") {
		level = enums.SeverityCritical
	} else if share < cfgFloat(cfg, "warn") {
		return nil
	}
	return []Finding{svcFinding(borgSvc, "borg.storage", "storage", "borg.storage", level, data.URL,
		map[string]any{"percent": int(share*percentScale + 0.5)})}
}

func borgUpdate(data *sources.BorgDataset, cfg map[string]any, env Env) []Finding {
	if !data.ServerUpdate && data.AgentsOutdated == 0 {
		return nil
	}
	return []Finding{svcFinding(borgSvc, "borg.update", "update", "borg.update", enums.SeverityInfo, data.URL,
		map[string]any{"agents": data.AgentsOutdated})}
}
