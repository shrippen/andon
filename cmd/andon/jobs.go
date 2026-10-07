package main

import (
	"context"
	"database/sql"
	"time"

	"andon/internal/db"
	"andon/internal/services/analysis"
	"andon/internal/services/audit"
	"andon/internal/services/auth"
	"andon/internal/services/hints"
	"andon/internal/services/homelable"
	"andon/internal/services/icons"
	"andon/internal/services/linkstatus"
	"andon/internal/services/notify"
	"andon/internal/services/scheduler"
	"andon/internal/services/selfbackup"
	"andon/internal/services/svcdata"
	"andon/internal/settings"
)

const (
	minute = time.Minute
	hour   = time.Hour
	day    = 24 * time.Hour
)

// backgroundJobs is the fixed job list.
func backgroundJobs(database *sql.DB, cfg settings.Settings) []scheduler.Job {
	return []scheduler.Job{
		{Name: analysis.JobName, Every: func() time.Duration { return time.Duration(settings.Live(cfg).AnalysisMinutes) * minute }, Start: scheduler.AtStart, Run: func(ctx context.Context) error {
			_, err := analysis.RunAll(ctx, database, time.Now().UTC())
			scheduler.Trigger(homelable.JobName) // draws what this run read
			return err
		}},
		{Name: homelable.JobName, Interval: day, Run: func(ctx context.Context) error {
			return homelable.SyncAll(ctx, database)
		}},
		{Name: "notify", Interval: minute, Run: func(ctx context.Context) error {
			_, err := notify.Dispatch(ctx, database, settings.Live(cfg))
			return err
		}},
		{Name: "digest", Interval: 5 * minute, Run: func(context.Context) error {
			_, err := notify.Digests(database, time.Now())
			return err
		}},
		{Name: linkstatus.JobName, Interval: linkstatus.Interval, Run: func(ctx context.Context) error {
			return linkstatus.Check(ctx, database)
		}},
		{Name: "icons", Interval: day, Run: func(context.Context) error {
			return icons.ForgetMisses()
		}},
		{Name: selfbackup.JobName, Interval: selfbackup.Check, Start: scheduler.AtStart, Run: func(context.Context) error {
			_, err := selfbackup.RunDue(database, cfg.BackupsDir(), time.Now())
			return err
		}},
		{Name: "housekeeping", Interval: hour, Run: func(context.Context) error {
			return housekeeping(database)
		}},
	}
}

func housekeeping(database *sql.DB) error {
	if err := auth.Purge(database); err != nil {
		return err
	}
	if err := svcdata.Prune(database); err != nil {
		return err
	}
	if err := hints.Prune(database); err != nil {
		return err
	}
	if err := analysis.PruneHistory(database, time.Now().UTC()); err != nil {
		return err
	}
	if err := analysis.PrunePoints(database, time.Now().UTC()); err != nil {
		return err
	}
	return db.WithTx(database, func(tx *sql.Tx) error { return audit.Prune(tx) })
}
