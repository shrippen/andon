package widgets

// "backups": one table over Borg, PG Back Web and TrueNAS snapshots of the
// space, worst first. Tools without a connection are simply missing.

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const defaultBackupHours = 26

// BackupsConfig is the "backups" widget's config.
type BackupsConfig struct {
	MaxHours     int
	Tools        []string // only these tools (service keys, lower case), empty = all
	OnlyProblems bool
	Days         int // days in the dot row
}

func init() {
	Tile[BackupsConfig]{Key: "backups", Category: CategoryInsight, Topic: TopicHomelab, RefreshS: 600, Extra: ExtraHistory,
		Fields: []Field{{Key: "max_hours", Input: InputNumber, Default: defaultBackupHours, Min: "1", Max: "336"}, {Key: "tools", Input: InputList},
			{Key: "only_problems", Input: InputCheck}, sel("days", strconv.Itoa(backupDays), "7", "14", "30")},
		Decode: func(r Raw) BackupsConfig {
			days, _ := strconv.Atoi(r.Pick("days"))
			return BackupsConfig{MaxHours: r.Int("max_hours"), Tools: r.Lower("tools"), OnlyProblems: r.Bool("only_problems"), Days: days}
		},
		Queries: backupsQueries, View: backupsView,
		Calm: func(v map[string]any) bool {
			lines, _ := v["Rows"].([]BackupLine)
			for _, l := range lines {
				if l.State != "ok" {
					return false
				}
			}
			return v["Total"] != 0 && v["Total"] != nil
		}}.add()
}

func backupsQueries(BackupsConfig) []Query {
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

func backupsView(cfg BackupsConfig, results map[string]any, _ ViewCtx) map[string]any {
	if cfg.Days == 0 {
		cfg.Days = backupDays
	}
	borg, _ := results[string(enums.ServiceBorgBackup)].(*sources.BorgDataset)
	pg, _ := results[string(enums.ServicePGBackWeb)].(*sources.PGBackDataset)
	nas, _ := results[string(enums.ServiceTrueNAS)].(*sources.TrueNASDataset)
	maxAge := time.Duration(cfg.MaxHours) * time.Hour
	now := time.Now().UTC()
	h, _ := results[HistorySlot].(*metrics.History)

	var lines []BackupLine
	total := 0
	for _, row := range metrics.Backups(borg, pg, nas, now, maxAge) {
		if len(cfg.Tools) > 0 && !slices.Contains(cfg.Tools, strings.ToLower(row.Tool)) {
			continue
		}
		total++
		if cfg.OnlyProblems && row.State == metrics.BackupOK {
			continue
		}
		line := BackupLine{BackupRow: row}
		if h != nil {
			for i, mark := range metrics.BackupDays(h, row.Tool, row.Item, now, cfg.Days) {
				cell := StripCell{State: "none", Title: now.AddDate(0, 0, i-cfg.Days+1).Format(time.DateOnly)}
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
	return map[string]any{"Rows": lines, "Total": total, "Wide": cfg.Days > backupDays}
}
