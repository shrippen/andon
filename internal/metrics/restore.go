package metrics

// The restore test of a quarter: one backup per system, picked the same
// way all quarter and another one the next, so tests go round
// (backups popup, backups.restore_untested).
//
//	Borg: the clients in turn · PG Back Web: the backups · TrueNAS: the snapshot tasks

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// RestoreSample is the backup to restore this quarter from data, "" when
// it names none.
func RestoreSample(data any, now time.Time) string {
	var names []string
	switch d := data.(type) {
	case *sources.BorgDataset:
		for _, c := range d.Clients {
			names = append(names, c.Name)
		}
	case *sources.PGBackDataset:
		for _, b := range d.Backups {
			names = append(names, b.Name)
		}
	case *sources.TrueNASDataset:
		for _, s := range d.Snapshots {
			if s.Enabled {
				names = append(names, s.Dataset)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	quarter := now.Year()*quartersPerYear + (int(now.Month())-1)/monthsPerQuarter
	return names[quarter%len(names)]
}

const (
	quartersPerYear  = 4
	monthsPerQuarter = 3
)
