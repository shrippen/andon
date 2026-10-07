package rules

// Heartbeats (Healthchecks) on their own and against the backups:
//
//	heartbeat.down           a check is down (late beyond its grace time)
//	cross.heartbeat_backup   a check and the backup it watches disagree:
//	                         "borg-laptop" ↔ Borg client "laptop" (the check's
//	                         name holds the backup item's name)
//	                           check up,   backup failed/old → the job pings although it failed
//	                           check down, backup fresh      → the ping is missing, not the backup

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var healthchecksSvc = string(enums.ServiceHealthchecks)

// minItemName keeps short item names ("db") from matching every check.
const minItemName = 3

func init() {
	Register("heartbeat.down", healthchecksSvc, nil, on(heartbeatDown))
	Register("cross.heartbeat_backup", Cross, map[string]any{"backup_hours": 26.0}, heartbeatBackup)
}

func heartbeatDown(data *sources.HealthchecksDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, c := range data.Down() {
		found = append(found, svcFinding(healthchecksSvc, "heartbeat.down", "down:"+c.Name, "heartbeat.down", enums.SeverityWarn,
			data.URL, map[string]any{"name": c.Name, "since": agoParam(c.LastPing)}))
	}
	return found
}

// agoParam is a hint parameter for "since": the time, or "–" for never.
func agoParam(t time.Time) any {
	if t.IsZero() {
		return "–"
	}
	return Day(t)
}

func heartbeatBackup(_ any, cfg map[string]any, env Env) []Finding {
	hc, ok := env.Datasets[healthchecksSvc].(*sources.HealthchecksDataset)
	if !ok {
		return nil
	}
	maxAge := time.Duration(cfgFloat(cfg, "backup_hours") * float64(time.Hour))
	rows := metrics.Backups(metrics.BackupTools(env.Datasets), time.Now().UTC(), maxAge)

	var found []Finding
	for _, c := range hc.Checks {
		row, ok := watched(c.Name, rows)
		if !ok {
			continue
		}
		params := map[string]any{"check": c.Name, "item": row.Item, "tool": map[string]any{"$t": "service." + row.Tool}, "day": agoParam(row.Last)}
		switch {
		case c.Status == sources.HeartbeatUp && (row.State == metrics.BackupFailed || row.State == metrics.BackupOld):
			found = append(found, Finding{Fingerprint: "ok_not:" + c.Name, Severity: enums.SeverityWarn, Message: "cross.heartbeat_ok_backup_not",
				Params: params, Sources: []string{healthchecksSvc, row.Tool}})
		case c.Status == sources.HeartbeatDown && row.State == metrics.BackupOK:
			found = append(found, Finding{Fingerprint: "down_ok:" + c.Name, Severity: enums.SeverityInfo, Message: "cross.heartbeat_down_backup_ok",
				Params: params, Sources: []string{healthchecksSvc, row.Tool}})
		}
	}
	return found
}

// watched finds the backup a check watches: the longest item name the
// check's name holds ("pgback-invoiceninja" → "invoiceninja").
func watched(check string, rows []metrics.BackupRow) (metrics.BackupRow, bool) {
	check = strings.ToLower(check)
	var best metrics.BackupRow
	for _, r := range rows {
		item := strings.ToLower(r.Item)
		if len(item) >= minItemName && strings.Contains(check, item) && len(item) > len(best.Item) {
			best = r
		}
	}
	return best, best.Item != ""
}
