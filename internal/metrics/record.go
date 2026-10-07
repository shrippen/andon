package metrics

// What each analysis run records from a scope's datasets. Every dataset
// type registers its own recorder next to its figures, the way rules
// register themselves; Read runs all of them:
//
//	Record(func(d *sources.KumaDataset, now time.Time, r *Readings) {
//		r.Count(key("kuma", "runs", m.Name), 1)
//	})
//
//	Scope{datasets, settings} ──Read──► Readings
//	  Values   the day's value, the last run wins     ──► samples
//	  Counts   added up over the day's runs           ──► samples
//	  Lows     the lowest of the day's runs           ──► samples
//	  Versions a change is an "update" event          ──► versions + events
//	  States   a change is a "change" event           ──► versions + events

import (
	"strconv"
	"time"
)

// Readings are one run's records of a scope.
type Readings struct {
	Values   map[string]float64
	Past     map[string]map[string]float64 // day → key → value
	Counts   map[string]float64
	Lows     map[string]float64
	Versions map[string]string
	States   map[string]string
}

// Set records the day's value of a series.
func (r *Readings) Set(key string, v float64) { r.Values[key] = v }

// SetOn records a past day's value of a series ("2026-09-30"): figures a
// service reports a day late, like Tibber's consumption.
func (r *Readings) SetOn(day, key string, v float64) {
	if r.Past[day] == nil {
		r.Past[day] = map[string]float64{}
	}
	r.Past[day][key] = v
}

// Count adds v to the day's total of a series.
func (r *Readings) Count(key string, v float64) { r.Counts[key] += v }

// Low records a value of which the day keeps the lowest.
func (r *Readings) Low(key string, v float64) {
	if old, ok := r.Lows[key]; !ok || v < old {
		r.Lows[key] = v
	}
}

// Version records a subject's running version; "" is none.
func (r *Readings) Version(subject, version string) {
	if version != "" {
		r.Versions[subject] = version
	}
}

// State records a subject's state; a change from the last run becomes an
// event ("public IP 1.2.3.4 → 5.6.7.8"). "" is unknown and records nothing.
func (r *Readings) State(subject, state string) {
	if state != "" {
		r.States[statePrefix+subject] = state
	}
}

// statePrefix keeps states apart from versions in the shared table.
const statePrefix = "state:"

// StateKey is a state's key in the versions table, for states recorded
// outside an analysis run (the public IP a tile sees).
func StateKey(subject string) string { return statePrefix + subject }

// Subjects of recorded changes.
const (
	SubjectIP      = "IPv4"
	SubjectVPN     = "VPN"
	SubjectVPNExit = "VPN-Exit"
)

// Scope is what recorders read: a scope's datasets (keyed by service) and
// its space's settings.
type Scope struct {
	Datasets map[string]any
	Settings map[string]any
}

// recorder reads one dataset (or, scope-wide, all of them).
type recorder func(s Scope, now time.Time, r *Readings)

var recorders []recorder

// Record registers what a dataset type contributes to the history.
func Record[D any](f func(d D, now time.Time, r *Readings)) {
	recorders = append(recorders, func(s Scope, now time.Time, r *Readings) {
		for _, raw := range s.Datasets {
			if d, ok := raw.(D); ok {
				f(d, now, r)
			}
		}
	})
}

// RecordScope registers a recorder that needs several datasets at once
// (backups across Borg, PG Back Web and TrueNAS) or the settings.
func RecordScope(f func(s Scope, now time.Time, r *Readings)) {
	recorders = append(recorders, f)
}

// Read runs every recorder over a scope.
func Read(s Scope, now time.Time) Readings {
	r := Readings{Values: map[string]float64{}, Past: map[string]map[string]float64{}, Counts: map[string]float64{}, Lows: map[string]float64{}, Versions: map[string]string{}, States: map[string]string{}}
	for _, rec := range recorders {
		rec(s, now, &r)
	}
	return r
}

// EventChange marks a state change (public IP, VPN exit, price);
// EventRestore a restore test marked by hand (subject: the service).
// EventClose a month seen complete in the month close (subject "2026-08").
const (
	EventChange  = "change"
	EventRestore = "restore"
	EventClose   = "close"
	EventFiled   = "filed" // a deadline handed in, subject DeadlineKey
)

// DeadlineKey names one deadline for its "filed" mark: "vat_return:2026-08".
func DeadlineKey(kind, period string, year int) string {
	return kind + ":" + period + ":" + strconv.Itoa(year)
}

// StateEvent turns a state change into a timeline event; a first
// sighting is none. The subject loses its table prefix.
func StateEvent(subject, old, now string, at time.Time) (Event, bool) {
	if old == "" || old == now {
		return Event{}, false
	}
	return Event{At: at, Kind: EventChange, Subject: subject[len(statePrefix):], Detail: old + " → " + now}, true
}
