package analysis

// Each run stores the scope's key figures and version changes, then
// hands the stored history to the rules:
//
//	datasets → metrics.Read → Values   → samples (one value per key and day)
//	                          Counts   → samples, added up per day (monitor uptime)
//	                          Versions → versions; a change → events ("update")
//	                          States   → versions; a change → events ("change")
//	samples (history.SeriesDays) + events (eventDays) → Datasets["history"]

import (
	"database/sql"
	"time"

	"andon/internal/db"
	"andon/internal/metrics"
	data "andon/internal/repos/data"
	"andon/internal/services/history"
	"andon/internal/widgets"
)

func ownerID(owner *int64) int64 {
	if owner == nil {
		return 0
	}
	return *owner
}

// recordHistory stores today's figures and version changes of a scope
// and returns its history for the rules.
func recordHistory(d *sql.DB, sc *scope, now time.Time) (*metrics.History, error) {
	owner := ownerID(sc.owner)
	day := now.Format(time.DateOnly)
	err := db.WithTx(d, func(tx *sql.Tx) error {
		read := metrics.Read(metrics.Scope{Datasets: sc.datasets, Settings: sc.settings}, now)
		if err := data.PutSamples(tx, sc.spaceID, owner, day, read.Values); err != nil {
			return err
		}
		if err := data.AddSamples(tx, sc.spaceID, owner, day, read.Counts); err != nil {
			return err
		}
		for past, values := range read.Past {
			if err := data.PutSamples(tx, sc.spaceID, owner, past, values); err != nil {
				return err
			}
		}
		known, err := data.Versions(tx, sc.spaceID, owner)
		if err != nil {
			return err
		}
		if err := recordChanges(tx, sc.spaceID, owner, known, read.Versions, metrics.VersionEvent, now); err != nil {
			return err
		}
		return recordChanges(tx, sc.spaceID, owner, known, read.States, metrics.StateEvent, now)
	})
	if err != nil {
		return nil, err
	}
	return history.Load(d, sc.spaceID, owner, now)
}

// recordChanges stores the current values of subjects and an event for
// each change the event function sees.
func recordChanges(tx *sql.Tx, spaceID, owner int64, known, current map[string]string,
	event func(subject, old, now string, at time.Time) (metrics.Event, bool), now time.Time) error {
	for subject, value := range current {
		if e, ok := event(subject, known[subject], value, now); ok {
			if err := data.AddEvent(tx, spaceID, data.Event{At: e.At, Kind: e.Kind, Subject: e.Subject, Detail: e.Detail, Owner: owner}); err != nil {
				return err
			}
		}
		if known[subject] == value {
			continue
		}
		if err := data.SetVersion(tx, spaceID, owner, subject, value, now); err != nil {
			return err
		}
	}
	return nil
}

// PruneHistory drops samples older than history.SeriesDays and events older
// than a year.
func PruneHistory(d *sql.DB, now time.Time) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		return data.PruneHistory(tx, now.AddDate(0, 0, -history.SeriesDays).Format(time.DateOnly), now.AddDate(-1, 0, 0))
	})
}

// PrunePoints drops daily key figures older than the longest trend span.
func PrunePoints(d *sql.DB, now time.Time) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		return data.PrunePoints(tx, now.AddDate(0, 0, -widgets.MaxTrendDays).Format(time.DateOnly))
	})
}

// recordLight counts, per run, the worst level among the space's shared
// open hints: over a day that is how often the status light was red.
//
//	light.runs  288   light.top.30  12   (critical in 12 runs)
func recordLight(d *sql.DB, spaceID int64, now time.Time) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		open, err := data.HintsIn(tx, []int64{spaceID}, 0)
		if err != nil {
			return err
		}
		top := 0
		for _, h := range open {
			top = max(top, int(h.Severity))
		}
		counts := map[string]float64{metrics.SampleKey("light", "runs"): 1}
		if top > 0 {
			counts[metrics.LightKey(top)] = 1
		}
		return data.AddSamples(tx, spaceID, 0, now.Format(time.DateOnly), counts)
	})
}
