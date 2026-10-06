package rules

import (
	"andon/internal/caps"
	"fmt"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
)

// BackupSystems are the services whose backups can be restored; each asks
// for one restore test a quarter.
var BackupSystems = caps.Restorable()

const monthsPerQuarter = 3

func init() {
	// A backup counts once it has been restored: a restore test marked in
	// the backups dialog (event "restore") quiets it for the quarter, and
	// so does acknowledging the hint. The fingerprint carries the quarter,
	// the next quarter brings a new one.
	//
	//	restore:borgbackup:2026-Q4   due 2026-12-31
	Register("backups.restore_untested", Cross, nil, restoreUntested)
}

func restoreUntested(_ any, _ map[string]any, env Env) []Finding {
	quarter := (int(env.Today.Month())-1)/monthsPerQuarter + 1
	end := time.Date(env.Today.Year(), time.Month(quarter*monthsPerQuarter)+1, 0, 0, 0, 0, 0, time.UTC)
	start := time.Date(env.Today.Year(), time.Month((quarter-1)*monthsPerQuarter)+1, 1, 0, 0, 0, 0, time.UTC)
	tested := map[string]bool{}
	for _, e := range historyOf(env).Events {
		if e.Kind == metrics.EventRestore && !e.At.Before(start) {
			tested[e.Subject] = true
		}
	}
	var found []Finding
	for _, svc := range BackupSystems {
		if _, ok := env.Datasets[string(svc)]; !ok || tested[string(svc)] {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("restore:%s:%d-Q%d", svc, env.Today.Year(), quarter),
			Rule:        "backups.restore_untested", Severity: enums.SeverityInfo, Message: "backups.restore_untested",
			Params: map[string]any{"system": map[string]any{"$t": "service." + string(svc)}, "day": Day(end)},
			Due:    end.Format(time.DateOnly), Sources: []string{string(svc)},
		})
	}
	return found
}
