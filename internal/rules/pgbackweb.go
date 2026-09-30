package rules

import (
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	Register("pgbackweb.failed", pgbackSvc, nil, on(pgbackwebFailed))

	// Only backups that succeeded before: a backup without success events
	// may simply have no success webhook configured.
	Register("pgbackweb.stale", pgbackSvc, map[string]any{"warn_hours": 26.0}, on(pgbackwebStale))

	Register("pgbackweb.unhealthy", pgbackSvc, nil, on(pgbackwebUnhealthy))

	Register("pgbackweb.silent", pgbackSvc, map[string]any{"days": 3.0}, on(pgbackwebSilent))
}

func pgbackwebFailed(d *sources.PGBackDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, b := range d.Backups {
		if !b.Failing() {
			continue
		}
		found = append(found, svcFinding(pgbackSvc, "pgbackweb.failed", "failed:"+b.Name, "pgbackweb.failed",
			enums.SeverityCritical, d.URL, map[string]any{"backup": b.Name, "day": Day(b.LastFailure)}))
	}
	return found
}

func pgbackwebStale(d *sources.PGBackDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, b := range d.Backups {
		hours := time.Now().UTC().Sub(b.LastSuccess).Hours()
		if b.LastSuccess.IsZero() || b.Failing() || hours < cfgFloat(cfg, "warn_hours") {
			continue
		}
		found = append(found, svcFinding(pgbackSvc, "pgbackweb.stale", "stale:"+b.Name, "pgbackweb.stale",
			enums.SeverityWarn, d.URL, map[string]any{"backup": b.Name, "hours": int(hours)}))
	}
	return found
}

func pgbackwebUnhealthy(d *sources.PGBackDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, h := range d.Unhealthy {
		found = append(found, svcFinding(pgbackSvc, "pgbackweb.unhealthy", "health:"+h.Kind+":"+h.Name, "pgbackweb.unhealthy",
			enums.SeverityWarn, d.URL, map[string]any{"name": h.Name, "kind": h.Kind, "day": Day(h.Since)}))
	}
	return found
}

func pgbackwebSilent(d *sources.PGBackDataset, cfg map[string]any, env Env) []Finding {
	if !d.LastEvent.IsZero() && time.Now().UTC().Sub(d.LastEvent).Hours()/hoursPerDay < cfgFloat(cfg, "days") {
		return nil
	}
	return []Finding{svcFinding(pgbackSvc, "pgbackweb.silent", "silent", "pgbackweb.silent", enums.SeverityInfo, d.URL,
		map[string]any{"days": cfgInt(cfg, "days")})}
}
