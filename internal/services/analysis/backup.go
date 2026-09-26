package analysis

import (
	"database/sql"
	"log/slog"
	"time"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/rules"
	"andon/internal/services/selfbackup"
)

const (
	backupRule = "system.backup"
	// backupLate leaves the daily copy two hours of slack.
	backupLate = 26 * time.Hour
)

// backupFindings warns when Andon's own copy is late or failed its
// restore test; nothing before the first run.
func backupFindings(d *sql.DB, now time.Time) ([]rules.Finding, error) {
	last, err := selfbackup.Last(d)
	if err != nil || last == nil {
		return nil, err
	}
	finding := rules.Finding{Rule: backupRule, ActionURL: "/admin/settings#backup", ActionLabel: "backup"}
	switch {
	case !last.OK:
		finding.Fingerprint, finding.Severity, finding.Message = "failed", enums.SeverityCritical, "system.backup_failed"
	case now.Sub(last.At) > backupLate:
		finding.Fingerprint, finding.Severity, finding.Message = "late", enums.SeverityWarn, "system.backup_late"
		finding.Params = map[string]any{"hours": int(now.Sub(last.At).Hours())}
	default:
		return nil, nil
	}
	return []rules.Finding{finding}, nil
}

// syncBackup puts the backup finding into every admin's own space.
func syncBackup(d *sql.DB, now time.Time) {
	findings, err := backupFindings(d, now)
	if err != nil {
		slog.Error("analysis: backup status", "err", err)
		return
	}
	all, err := users.All(d)
	if err != nil {
		slog.Error("analysis: backup admins", "err", err)
		return
	}
	for _, u := range all {
		if u.Role != enums.RoleAdmin || !u.IsActive {
			continue
		}
		space, err := content.PersonalSpace(d, u.ID)
		if err != nil || space == nil {
			continue
		}
		if _, err := syncHints(d, space.ID, &u.ID, nil, []string{backupRule}, findings); err != nil {
			slog.Error("analysis: backup hint", "user", u.ID, "err", err)
		}
	}
}
