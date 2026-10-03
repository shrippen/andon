package metrics

// Backup overview across tools:
//
//	borg clients   ─┐
//	pgbackweb      ─┼─► []BackupRow{Tool, Item, Last, State}  (oldest first)
//	truenas snaps  ─┘

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// BackupState is a row's outcome.
type BackupState string

const (
	BackupOK      BackupState = "ok"
	BackupOld     BackupState = "old"
	BackupFailed  BackupState = "failed"
	BackupUnknown BackupState = "unknown"
)

// BackupRow is one backed-up item.
type BackupRow struct {
	Tool, Item string
	Last       time.Time // zero: never / unknown
	State      BackupState
}

// Backups builds the overview; maxAge marks older successes as old.
func Backups(borg *sources.BorgDataset, pg *sources.PGBackDataset, nas *sources.TrueNASDataset, now time.Time, maxAge time.Duration) []BackupRow {
	var rows []BackupRow
	age := func(last time.Time) BackupState {
		switch {
		case last.IsZero():
			return BackupUnknown
		case now.Sub(last) > maxAge:
			return BackupOld
		}
		return BackupOK
	}

	if borg != nil {
		for _, c := range borg.Clients {
			row := BackupRow{Tool: "borgbackup", Item: c.Name, Last: c.LastBackup, State: age(c.LastBackup)}
			if c.LastFailed {
				row.State = BackupFailed
			}
			rows = append(rows, row)
		}
	}
	if pg != nil {
		for _, b := range pg.Backups {
			row := BackupRow{Tool: "pgbackweb", Item: b.Name, Last: b.LastSuccess, State: age(b.LastSuccess)}
			if b.Failing() {
				row.State = BackupFailed
			}
			rows = append(rows, row)
		}
	}
	if nas != nil {
		for _, s := range nas.Snapshots {
			if !s.Enabled {
				continue
			}
			row := BackupRow{Tool: "truenas", Item: s.Dataset, Last: s.Last, State: age(s.Last)}
			if s.State == "ERROR" {
				row.State = BackupFailed
			}
			rows = append(rows, row)
		}
	}

	// Worst first: failed, old, unknown, ok; then oldest.
	rank := map[BackupState]int{BackupFailed: 0, BackupOld: 1, BackupUnknown: 2, BackupOK: 3}
	sort.SliceStable(rows, func(i, j int) bool {
		if rank[rows[i].State] != rank[rows[j].State] {
			return rank[rows[i].State] < rank[rows[j].State]
		}
		return rows[i].Last.Before(rows[j].Last)
	})
	return rows
}

// Per backup item, 1 when its newest backup falls on now's day, else 0.
// Stored once a day (the last run wins), the marks give the item's history
// even though the tools report only the newest backup.
func init() {
	RecordScope(func(s Scope, now time.Time, r *Readings) {
		var borg *sources.BorgDataset
		var pg *sources.PGBackDataset
		var nas *sources.TrueNASDataset
		for _, raw := range s.Datasets {
			switch d := raw.(type) {
			case *sources.BorgDataset:
				borg = d
			case *sources.PGBackDataset:
				pg = d
			case *sources.TrueNASDataset:
				nas = d
			}
		}
		if borg == nil && pg == nil && nas == nil {
			return
		}
		for _, row := range Backups(borg, pg, nas, now, 0) {
			mark := 0.0
			if !row.Last.IsZero() && Today(row.Last).Equal(Today(now)) {
				mark = 1
			}
			r.Set(backupKey(row.Tool, row.Item), mark)
		}
	})
}

// backupKey keeps the item's own spelling apart from key()'s dot rule.
func backupKey(tool, item string) string { return key("backup", tool, item) }

// BackupDays reads an item's marks for the last n days, oldest first:
// 1 backed up, 0 not, -1 not recorded.
func BackupDays(h *History, tool, item string, now time.Time, n int) []float64 {
	marks := map[time.Time]float64{}
	for _, p := range h.SeriesOf(backupKey(tool, item)) {
		marks[Today(p.Day)] = p.Value
	}
	out := make([]float64, n)
	for i := range out {
		day := Today(now).AddDate(0, 0, i-n+1)
		out[i] = -1
		if v, ok := marks[day]; ok {
			out[i] = v
		}
	}
	return out
}
