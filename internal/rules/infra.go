package rules

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// Alert levels as TrueNAS and Komodo report them.
var alertSeverity = map[string]enums.Severity{
	"WARNING":  enums.SeverityWarn,
	"ERROR":    enums.SeverityCritical,
	"CRITICAL": enums.SeverityCritical,
}

func init() {
	registerTrueNAS()
	registerKomodo()
	registerPangolin()
	registerAuthentik()
}

func registerTrueNAS() {
	Register("truenas.pool_unhealthy", truenasSvc, nil, on(poolUnhealthy))

	// ZFS slows down noticeably above 80 % fill.
	Register("truenas.pool_full", truenasSvc, map[string]any{"warn": 0.8, "critical": 0.9}, on(poolFull))

	Register("truenas.alerts", truenasSvc, nil, on(truenasAlerts))

	Register("truenas.app_updates", truenasSvc, nil, on(truenasAppUpdates))
}

func poolUnhealthy(data *sources.TrueNASDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, p := range data.Pools {
		if p.Healthy {
			continue
		}
		found = append(found, svcFinding(truenasSvc, "truenas.pool_unhealthy", "pool:"+p.Name, "truenas.pool_unhealthy",
			enums.SeverityCritical, data.URL, map[string]any{"pool": p.Name, "status": p.Status}))
	}
	return found
}

func poolFull(data *sources.TrueNASDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, p := range data.Pools {
		if p.Size <= 0 {
			continue
		}
		share := p.Allocated / p.Size
		level := enums.SeverityWarn
		if share >= cfgFloat(cfg, "critical") {
			level = enums.SeverityCritical
		} else if share < cfgFloat(cfg, "warn") {
			continue
		}
		found = append(found, svcFinding(truenasSvc, "truenas.pool_full", "full:"+p.Name, "truenas.pool_full",
			level, data.URL, map[string]any{"pool": p.Name, "percent": int(share*percentScale + 0.5)}))
	}
	return found
}

func truenasAlerts(data *sources.TrueNASDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Alerts {
		level, ok := alertSeverity[strings.ToUpper(a.Level)]
		if !ok {
			continue
		}
		found = append(found, svcFinding(truenasSvc, "truenas.alerts", "alert:"+a.ID, "truenas.alert",
			level, data.URL, map[string]any{"text": a.Text}))
	}
	return found
}

func truenasAppUpdates(data *sources.TrueNASDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, a := range data.Apps {
		if a.Update {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(truenasSvc, "truenas.app_updates", "apps", "truenas.app_updates", enums.SeverityInfo,
		strings.TrimRight(data.URL, "/")+"/ui/apps", map[string]any{"count": len(names), "apps": shortList(names)})}
}

// Stack states Komodo reports for a stack that should run but does not.
var komodoBroken = map[string]bool{"down": true, "unhealthy": true, "dead": true, "restarting": true}

func registerKomodo() {
	Register("komodo.alerts", komodoSvc, nil, on(komodoAlerts))

	Register("komodo.stack_down", komodoSvc, nil, on(stackDown))

	Register("komodo.updates", komodoSvc, nil, on(komodoUpdates))
}

func komodoAlerts(data *sources.KomodoDataset, cfg map[string]any, env Env) []Finding {
	stopped := metrics.KomodoStopped(env.Options[komodoSvc])
	var found []Finding
	for _, a := range data.Alerts {
		level, ok := alertSeverity[a.Level]
		if !ok || stopped[strings.ToLower(a.Name)] {
			continue
		}
		found = append(found, svcFinding(komodoSvc, "komodo.alerts", "alert:"+a.Kind+":"+a.Name, "komodo.alert",
			level, data.URL, map[string]any{"kind": a.Kind, "name": a.Name}))
	}
	return found
}

func stackDown(data *sources.KomodoDataset, cfg map[string]any, env Env) []Finding {
	stopped := metrics.KomodoStopped(env.Options[komodoSvc])
	var found []Finding
	for _, s := range data.Stacks {
		if !komodoBroken[s.State] || stopped[strings.ToLower(s.Name)] {
			continue
		}
		found = append(found, svcFinding(komodoSvc, "komodo.stack_down", "stack:"+s.Name, "komodo.stack_down",
			enums.SeverityWarn, strings.TrimRight(data.URL, "/")+"/stacks", map[string]any{"stack": s.Name, "state": s.State}))
	}
	return found
}

func komodoUpdates(data *sources.KomodoDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, s := range data.Stacks {
		if len(s.Updates) > 0 {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(komodoSvc, "komodo.updates", "updates", "komodo.updates", enums.SeverityInfo,
		strings.TrimRight(data.URL, "/")+"/stacks", map[string]any{"count": len(names), "stacks": shortList(names)})}
}

func registerPangolin() {
	Register("pangolin.site_offline", pangolinSvc, nil, on(siteOffline))

	Register("pangolin.unhealthy", pangolinSvc, nil, on(pangolinUnhealthy))

	Register("pangolin.newt_update", pangolinSvc, nil, on(newtUpdate))
}

func siteOffline(data *sources.PangolinDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, s := range data.Sites {
		if s.Online == nil || *s.Online {
			continue
		}
		found = append(found, svcFinding(pangolinSvc, "pangolin.site_offline", "site:"+s.Name, "pangolin.site_offline",
			enums.SeverityCritical, data.URL, map[string]any{"site": s.Name}))
	}
	return found
}

func pangolinUnhealthy(data *sources.PangolinDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, r := range data.Resources {
		if !r.Enabled || r.Health != "unhealthy" {
			continue
		}
		found = append(found, svcFinding(pangolinSvc, "pangolin.unhealthy", "res:"+r.Domain, "pangolin.unhealthy",
			enums.SeverityWarn, "https://"+r.Domain, map[string]any{"name": r.Name, "domain": r.Domain}))
	}
	return found
}

func newtUpdate(data *sources.PangolinDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, s := range data.Sites {
		if s.Update {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(pangolinSvc, "pangolin.newt_update", "newt", "pangolin.newt_update", enums.SeverityInfo,
		data.URL, map[string]any{"sites": shortList(names)})}
}

func registerAuthentik() {
	Register("authentik.update", authentikSvc, nil, on(authentikUpdate))

	// Many failed logins within a day hint at guessing or a broken client.
	Register("authentik.failed_logins", authentikSvc, map[string]any{"warn": 20.0, "critical": 100.0}, on(failedLogins))

	// Accounts nobody uses are attack surface.
	Register("authentik.stale_users", authentikSvc, map[string]any{"days": 180.0}, on(staleUsers))
}

// authentikURL links a page of the admin UI.
func authentikURL(data *sources.AuthentikDataset, path string) string {
	return strings.TrimRight(data.URL, "/") + "/if/admin/#/" + path
}

func authentikUpdate(data *sources.AuthentikDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	if data.Outdated {
		found = append(found, svcFinding(authentikSvc, "authentik.update", "update:"+data.Latest, "authentik.update",
			enums.SeverityInfo, authentikURL(data, "administration/overview"), map[string]any{"version": data.Latest, "current": data.Version}))
	}
	if data.Outposts {
		found = append(found, svcFinding(authentikSvc, "authentik.update", "outposts", "authentik.outposts",
			enums.SeverityWarn, authentikURL(data, "outpost/outposts"), nil))
	}
	return found
}

func failedLogins(data *sources.AuthentikDataset, cfg map[string]any, env Env) []Finding {
	n := float64(data.Failed24h)
	level := enums.SeverityWarn
	if n >= cfgFloat(cfg, "critical") {
		level = enums.SeverityCritical
	} else if n < cfgFloat(cfg, "warn") {
		return nil
	}
	return []Finding{svcFinding(authentikSvc, "authentik.failed_logins", "failed", "authentik.failed_logins", level,
		authentikURL(data, "events/log"), map[string]any{"count": data.Failed24h})}
}

func staleUsers(data *sources.AuthentikDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, u := range data.Users {
		if !u.LastLogin.IsZero() && env.Today.Sub(u.LastLogin).Hours()/hoursPerDay <= cfgFloat(cfg, "days") {
			continue
		}
		names = append(names, u.Name)
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(authentikSvc, "authentik.stale_users", "stale", "authentik.stale_users", enums.SeverityInfo,
		authentikURL(data, "identity/users"), map[string]any{"count": len(names), "users": shortList(names), "days": cfgInt(cfg, "days")})}
}
