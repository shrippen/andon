package rules

import (
	"fmt"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const hoursPerDay = 24

// expiryLevel grades days left against info/warn/critical thresholds;
// ok is false when the date is still far away.
func expiryLevel(daysLeft int, cfg map[string]any) (enums.Severity, bool) {
	switch {
	case daysLeft <= cfgInt(cfg, "critical_days"):
		return enums.SeverityCritical, true
	case daysLeft <= cfgInt(cfg, "warn_days"):
		return enums.SeverityWarn, true
	case daysLeft <= cfgInt(cfg, "info_days"):
		return enums.SeverityInfo, true
	}
	return 0, false
}

// idSet reads a list of ids that YAML may give as numbers or strings.
func idSet(v any) map[string]bool {
	out := map[string]bool{}
	list, _ := v.([]any)
	for _, item := range list {
		switch x := item.(type) {
		case string:
			out[strings.TrimSpace(x)] = true
		case float64:
			out[strconv.FormatFloat(x, 'f', -1, 64)] = true
		}
	}
	return out
}

var expiryDefaults = map[string]any{"info_days": 30.0, "warn_days": 14.0, "critical_days": 3.0}

func init() {
	Register("kuma.monitor_down", kumaSvc, nil, on(monitorDown))

	Register("kuma.cert_expiring", kumaSvc, expiryDefaults, on(certExpiring))

	registerProxmox()

	Register("paperless.inbox", paperlessSvc, map[string]any{"min_count": 1.0, "warn_days": 14.0}, on(paperlessInbox))

	Register("certs.expiring", certsSvc, expiryDefaults, on(certsExpiring))
}

func monitorDown(data *sources.KumaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, m := range data.Monitors {
		if m.Status != sources.KumaDown {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "down:" + m.Name, Severity: enums.SeverityCritical,
			Message: "kuma.down", Params: map[string]any{"monitor": m.Name},
			ActionURL: data.URL, ActionLabel: "open_in_uptimekuma", Sources: []string{kumaSvc},
		})
	}
	return found
}

func certExpiring(data *sources.KumaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, m := range data.Monitors {
		level, ok := expiryLevel(m.CertDays, cfg)
		if m.CertDays < 0 || !ok {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "cert:" + m.Name, Severity: level,
			Message: "kuma.cert", Params: map[string]any{"monitor": m.Name, "days": m.CertDays},
			ActionURL: data.URL, ActionLabel: "open_in_uptimekuma", Sources: []string{kumaSvc},
		})
	}
	return found
}

func paperlessInbox(data *sources.PaperlessDataset, cfg map[string]any, env Env) []Finding {
	if data.Inbox < cfgInt(cfg, "min_count") || data.Inbox == 0 {
		return nil
	}
	level, days := enums.SeverityInfo, 0
	if added, ok := metrics.ParseDay(data.OldestAdded); ok {
		days = int(env.Today.Sub(added).Hours() / hoursPerDay)
	}
	if days >= cfgInt(cfg, "warn_days") {
		level = enums.SeverityWarn
	}
	return []Finding{{
		Fingerprint: "inbox", Severity: level, Message: "paperless.inbox",
		Params:    map[string]any{"count": data.Inbox, "title": data.OldestTitle, "days": days},
		ActionURL: strings.TrimRight(data.URL, "/") + "/documents?sort=added", ActionLabel: "open_in_paperless",
		Sources: []string{paperlessSvc},
	}}
}

func certsExpiring(data *sources.CertDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, c := range data.Certs {
		if c.Error != "" {
			found = append(found, Finding{
				Fingerprint: "unreachable:" + c.Host, Severity: enums.SeverityWarn,
				Message: "certs.unreachable", Params: map[string]any{"host": c.Host, "error": c.Error},
				Sources: []string{certsSvc},
			})
			continue
		}
		daysLeft := int(c.NotAfter.Sub(env.Today).Hours() / hoursPerDay)
		level, ok := expiryLevel(daysLeft, cfg)
		if !ok {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "cert:" + c.Host, Severity: level, Message: "certs.expiring",
			Params: map[string]any{"host": c.Host, "day": Day(c.NotAfter), "days": daysLeft},
			Due:    c.NotAfter.Format("2006-01-02"), Sources: []string{certsSvc},
		})
	}
	return found
}

func registerProxmox() {
	Register("proxmox.node_offline", proxmoxSvc, nil, on(nodeOffline))

	Register("proxmox.updates", proxmoxSvc, map[string]any{"min_count": 1.0}, on(proxmoxUpdates))

	Register("proxmox.storage_full", proxmoxSvc, map[string]any{"warn": 0.8, "critical": 0.9}, on(storageFull))

	Register("proxmox.backup_old", proxmoxSvc, map[string]any{"days": 2.0, "ignore": []any{}}, on(proxmoxBackupOld))
}

// proxmoxFinding is a finding linking the Proxmox host.
func proxmoxFinding(data *sources.ProxmoxDataset, rule, fp, msg string, level enums.Severity, params map[string]any) Finding {
	return Finding{Fingerprint: fp, Rule: rule, Severity: level, Message: msg, Params: params,
		ActionURL: data.URL, ActionLabel: "open_in_proxmox", Sources: []string{proxmoxSvc}}
}

func nodeOffline(data *sources.ProxmoxDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, n := range data.Nodes {
		if !n.Online {
			found = append(found, proxmoxFinding(data, "proxmox.node_offline", "offline:"+n.Name, "proxmox.offline",
				enums.SeverityCritical, map[string]any{"node": n.Name}))
		}
	}
	return found
}

func proxmoxUpdates(data *sources.ProxmoxDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, n := range data.Nodes {
		if n.Updates < cfgInt(cfg, "min_count") || n.Updates <= 0 {
			continue
		}
		found = append(found, proxmoxFinding(data, "proxmox.updates", "updates:"+n.Name, "proxmox.updates",
			enums.SeverityInfo, map[string]any{"node": n.Name, "count": n.Updates}))
	}
	return found
}

func storageFull(data *sources.ProxmoxDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, n := range data.Nodes {
		for _, s := range n.Storages {
			share := s.Used / s.Total
			level := enums.SeverityWarn
			if share >= cfgFloat(cfg, "critical") {
				level = enums.SeverityCritical
			} else if share < cfgFloat(cfg, "warn") {
				continue
			}
			found = append(found, proxmoxFinding(data, "proxmox.storage_full", fmt.Sprintf("storage:%s:%s", n.Name, s.Name),
				"proxmox.storage", level, map[string]any{"node": n.Name, "storage": s.Name, "percent": int(share*100 + 0.5)}))
		}
	}
	return found
}

func proxmoxBackupOld(data *sources.ProxmoxDataset, cfg map[string]any, env Env) []Finding {
	ignore := idSet(cfg["ignore"])

	var found []Finding
	for _, g := range data.Guests {
		vmid := strconv.FormatInt(g.VMID, 10)
		if g.Template || ignore[vmid] {
			continue
		}
		fp := "backup:" + vmid
		last, ok := data.Backups[g.VMID]
		if fromPBS := pbsLast(env, vmid); fromPBS.After(last) {
			last, ok = fromPBS, true
		}
		if !ok {
			found = append(found, proxmoxFinding(data, "proxmox.backup_old", fp, "proxmox.backup_missing",
				enums.SeverityWarn, map[string]any{"guest": g.Name, "vmid": vmid}))
			continue
		}
		if env.Today.Sub(last).Hours()/hoursPerDay <= cfgFloat(cfg, "days") {
			continue
		}
		found = append(found, proxmoxFinding(data, "proxmox.backup_old", fp, "proxmox.backup_old",
			enums.SeverityWarn, map[string]any{"guest": g.Name, "vmid": vmid, "day": Day(last)}))
	}
	return found
}
