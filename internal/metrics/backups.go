package metrics

// Backup overview across tools:
//
//	sources.BackupSource (Borg, PG Back Web, TrueNAS, …) ─► []BackupRow{Tool, Item, Last, State}  (worst first)

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

// Backups builds the overview over every tool's jobs; maxAge marks
// older successes as old.
func Backups(tools []sources.BackupSource, now time.Time, maxAge time.Duration) []BackupRow {
	var rows []BackupRow
	for _, tool := range tools {
		for _, j := range tool.BackupJobs() {
			row := BackupRow{Tool: tool.BackupTool(), Item: j.Item, Last: j.Last, State: BackupOK}
			switch {
			case j.Failed:
				row.State = BackupFailed
			case j.Last.IsZero():
				row.State = BackupUnknown
			case now.Sub(j.Last) > maxAge:
				row.State = BackupOld
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
		tools := BackupTools(s.Datasets)
		if len(tools) == 0 {
			return
		}
		for _, row := range Backups(tools, now, 0) {
			mark := 0.0
			if !row.Last.IsZero() && Today(row.Last).Equal(Today(now)) {
				mark = 1
			}
			r.Set(backupKey(row.Tool, row.Item), mark)
		}
	})
}

// BackupTools picks the datasets that report backups, in a stable order.
func BackupTools(datasets map[string]any) []sources.BackupSource {
	var out []sources.BackupSource
	for _, raw := range datasets {
		if b, ok := raw.(sources.BackupSource); ok {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BackupTool() < out[j].BackupTool() })
	return out
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
