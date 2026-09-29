package rules

import (
	"fmt"
	"time"

	"andon/internal/enums"
)

// backupSystems are the services whose backups can be restored; each
// asks for one restore test a quarter.
var backupSystems = []enums.ServiceType{enums.ServiceBorgBackup, enums.ServicePGBackWeb, enums.ServiceTrueNAS, enums.ServiceProxmox}

const monthsPerQuarter = 3

func init() {
	// A backup counts once it has been restored. The fingerprint carries
	// the quarter: acknowledging the hint means "tested this quarter", and
	// the next quarter brings a new one.
	//
	//	restore:borgbackup:2026-Q4   due 2026-12-31
	Register("backups.restore_untested", Cross, nil, restoreUntested)
}

func restoreUntested(_ any, _ map[string]any, env Env) []Finding {
	quarter := (int(env.Today.Month())-1)/monthsPerQuarter + 1
	end := time.Date(env.Today.Year(), time.Month(quarter*monthsPerQuarter)+1, 0, 0, 0, 0, 0, time.UTC)
	var found []Finding
	for _, svc := range backupSystems {
		if _, ok := env.Datasets[string(svc)]; !ok {
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
