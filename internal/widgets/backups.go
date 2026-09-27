package widgets

// "backups": one table over Borg, PG Back Web and TrueNAS snapshots of the
// space, worst first. Tools without a connection are simply missing.

import (
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const defaultBackupHours = 26

type BackupsConfig struct{ MaxHours int }

func decodeBackups(raw map[string]any) any {
	return BackupsConfig{MaxHours: clampInt(asInt(raw["max_hours"], defaultBackupHours), 1, 24*14)}
}

func backupsQueries(any) []Query {
	return []Query{
		{Name: string(enums.ServiceBorgBackup), Source: "data", Conn: ConnPeer, Service: enums.ServiceBorgBackup},
		{Name: string(enums.ServicePGBackWeb), Source: "data", Conn: ConnPeer, Service: enums.ServicePGBackWeb},
		{Name: string(enums.ServiceTrueNAS), Source: "data", Conn: ConnPeer, Service: enums.ServiceTrueNAS},
	}
}

// BackupLine is one backup item with its last backupDays days (see
// metrics.BackupMarks).
type BackupLine struct {
	metrics.BackupRow
	Days []StripCell
}

const backupDays = 14

func backupsView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	borg, _ := results[string(enums.ServiceBorgBackup)].(*sources.BorgDataset)
	pg, _ := results[string(enums.ServicePGBackWeb)].(*sources.PGBackDataset)
	nas, _ := results[string(enums.ServiceTrueNAS)].(*sources.TrueNASDataset)
	maxAge := time.Duration(cfgAny.(BackupsConfig).MaxHours) * time.Hour
	now := time.Now().UTC()
	h, _ := results[HistorySlot].(*metrics.History)

	var lines []BackupLine
	for _, row := range metrics.Backups(borg, pg, nas, now, maxAge) {
		line := BackupLine{BackupRow: row}
		if h != nil {
			for i, mark := range metrics.BackupDays(h, row.Tool, row.Item, now, backupDays) {
				cell := StripCell{State: "none", Title: now.AddDate(0, 0, i-backupDays+1).Format(time.DateOnly)}
				switch mark {
				case 1:
					cell.State = "ok"
				case 0:
					cell.State = "miss"
				}
				line.Days = append(line.Days, cell)
			}
		}
		lines = append(lines, line)
	}
	return map[string]any{"Rows": lines}
}

func init() {
	Register(WidgetType{Key: "backups", Decode: decodeBackups, Template: "widgets/backups", Category: CategoryInsight,
		RefreshS: 600, Queries: backupsQueries, View: backupsView, Extra: ExtraHistory})
}
