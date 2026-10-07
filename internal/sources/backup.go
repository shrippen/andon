package sources

// Backups across tools: a dataset that knows backups names them in one
// shape, so the backup tile, its rules, the history and the update
// window read every tool the same way, also ones added later.
//
//	BorgDataset, PGBackDataset, TrueNASDataset, … ─► BackupTool() + BackupJobs()

import (
	"slices"
	"time"

	"andon/internal/enums"
)

// BackupJob is one backed-up item as its tool reports it.
type BackupJob struct {
	Item   string    // client, database, dataset, VM …
	Last   time.Time // newest successful backup, zero = never or unknown
	Failed bool      // the newest run failed
}

// BackupSource is a dataset that reports backups.
type BackupSource interface {
	BackupTool() string // service key, e.g. "borgbackup"
	BackupJobs() []BackupJob
}

// backupServices are the services whose dataset is a BackupSource.
var backupServices []enums.ServiceType

// registerBackup marks a service's dataset as a BackupSource.
func registerBackup(s enums.ServiceType) {
	if !slices.Contains(backupServices, s) {
		backupServices = append(backupServices, s)
	}
}

// BackupServices lists the services that report backups, for the tile's
// queries.
func BackupServices() []enums.ServiceType { return slices.Clone(backupServices) }

func (d *BorgDataset) BackupTool() string { return string(enums.ServiceBorgBackup) }

func (d *BorgDataset) BackupJobs() []BackupJob {
	out := make([]BackupJob, 0, len(d.Clients))
	for _, c := range d.Clients {
		out = append(out, BackupJob{Item: c.Name, Last: c.LastBackup, Failed: c.LastFailed})
	}
	return out
}

func (d *PGBackDataset) BackupTool() string { return string(enums.ServicePGBackWeb) }

func (d *PGBackDataset) BackupJobs() []BackupJob {
	out := make([]BackupJob, 0, len(d.Backups))
	for _, b := range d.Backups {
		out = append(out, BackupJob{Item: b.Name, Last: b.LastSuccess, Failed: b.Failing()})
	}
	return out
}

// snapError is a TrueNAS snapshot task's failed state.
const snapError = "ERROR"

func (d *TrueNASDataset) BackupTool() string { return string(enums.ServiceTrueNAS) }

// BackupJobs are the enabled periodic snapshot tasks.
func (d *TrueNASDataset) BackupJobs() []BackupJob {
	var out []BackupJob
	for _, s := range d.Snapshots {
		if s.Enabled {
			out = append(out, BackupJob{Item: s.Dataset, Last: s.Last, Failed: s.State == snapError})
		}
	}
	return out
}

func init() {
	for _, s := range []enums.ServiceType{enums.ServiceBorgBackup, enums.ServicePGBackWeb, enums.ServiceTrueNAS} {
		registerBackup(s)
	}
}
