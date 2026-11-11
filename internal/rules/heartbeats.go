package rules

// Heartbeats (every sources.HeartbeatSource, e.g. Healthchecks) on their own and against the backups:
//
//	heartbeat.down           a check is down (late beyond its grace time, or
//	                         reported as failed)
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
	Register("heartbeat.down", Cross, nil, heartbeatDown)
	Needs("heartbeat.down", healthchecksSvc, jidokaSvc)
	Register("cross.heartbeat_backup", Cross, map[string]any{"backup_hours": 26.0}, heartbeatBackup)
}

// heartbeatMessage is the hint text of a down job by its reason; a
// missed ping is the default.
var heartbeatMessage = map[sources.HeartbeatReason]string{
	sources.HeartbeatFailed: "heartbeat.failed", sources.HeartbeatMissed: "heartbeat.down",
}

func heartbeatDown(_ any, _ map[string]any, env Env) []Finding {
	var found []Finding
	for _, c := range sources.DownBeats(sources.HeartbeatsOf(env.Datasets)) {
		msg := heartbeatMessage[c.Reason]
		if msg == "" {
			msg = "heartbeat.down"
		}
		f := svcFinding(c.Tool, "heartbeat.down", "down:"+c.Name, msg, enums.SeverityWarn,
			c.Link, map[string]any{"name": c.Name, "since": agoParam(c.LastPing), "summary": c.Summary})
		f.ConnID = c.ConnID
		found = append(found, f)
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
	beats := sources.HeartbeatsOf(env.Datasets)
	maxAge := time.Duration(cfgFloat(cfg, "backup_hours") * float64(time.Hour))
	rows := metrics.Backups(metrics.BackupTools(env.Datasets), time.Now().UTC(), maxAge)

	var found []Finding
	for _, c := range beats {
		row, ok := watched(c.Name, rows)
		if !ok {
			continue
		}
		params := map[string]any{"check": c.Name, "item": row.Item, "tool": map[string]any{"$t": "service." + row.Tool}, "day": agoParam(row.Last)}
		switch {
		case c.Status == sources.HeartbeatUp && (row.State == metrics.BackupFailed || row.State == metrics.BackupOld):
			found = append(found, Finding{Fingerprint: "ok_not:" + c.Name, Severity: enums.SeverityWarn, Message: "cross.heartbeat_ok_backup_not",
				Params: params, Sources: []string{c.Tool, row.Tool}})
		case c.Status == sources.HeartbeatDown && row.State == metrics.BackupOK:
			found = append(found, Finding{Fingerprint: "down_ok:" + c.Name, Severity: enums.SeverityInfo, Message: "cross.heartbeat_down_backup_ok",
				Params: params, Sources: []string{c.Tool, row.Tool}})
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
