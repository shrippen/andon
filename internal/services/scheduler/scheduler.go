// Package scheduler runs the background jobs (one process, one scheduler):
//
//	every 5 min   fetch integrations, rules → hints (analysis; also at start,
//	              interval ANALYSIS_MINUTES)
//	every 1 min   push notifications
//	every 5 min   digest mails
//	hourly        housekeeping (sessions, cache, hints, audit)
//	hourly        own backup, when the last copy is a day old
//	daily         retry icons that failed to download
package scheduler

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
)

// StartMode says whether a job also runs right at startup.
type StartMode int

const (
	AfterInterval StartMode = iota // first run after one interval
	AtStart                        // first run immediately
)

// Job is one background task, run on its own interval.
type Job struct {
	Name     string
	Interval time.Duration
	Start    StartMode
	Run      func(context.Context) error
}

// Run is the outcome of a job's latest run.
type Run struct {
	At       time.Time
	Duration time.Duration
	Err      string
	Every    time.Duration
}

var (
	runsMu   sync.Mutex
	runs     = map[string]Run{}
	triggers = map[string]chan struct{}{}
)

// Trigger asks a job to run now (queued behind a running one); false if
// no such job runs.
func Trigger(name string) bool {
	runsMu.Lock()
	ch, ok := triggers[name]
	runsMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- struct{}{}:
	default: // one run is already queued
	}
	return true
}

// LastRun returns a job's latest run, false if it has not run yet.
func LastRun(name string) (Run, bool) {
	runsMu.Lock()
	defer runsMu.Unlock()
	r, ok := runs[name]
	return r, ok
}

// NamedRun is a job's latest run with the job's name.
type NamedRun struct {
	Name string
	Run
}

// Runs lists the latest run of every job that ran, by name.
func Runs() []NamedRun {
	runsMu.Lock()
	defer runsMu.Unlock()

	out := make([]NamedRun, 0, len(runs))
	for name, r := range runs {
		out = append(out, NamedRun{Name: name, Run: r})
	}
	slices.SortFunc(out, func(a, b NamedRun) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Start runs every job on its own timer until ctx is cancelled. Each job
// gets its own goroutine and runs strictly one at a time (the next wait,
// interval ± jitter, starts after a run ends); a panicking or erroring
// job is logged and never stops the others.
//
// The returned wait blocks until every job has stopped after ctx ends,
// so a running job finishes before the database closes.
func Start(ctx context.Context, jobs []Job) (wait func()) {
	var running sync.WaitGroup
	for _, job := range jobs {
		kick := make(chan struct{}, 1)
		runsMu.Lock()
		triggers[job.Name] = kick
		runsMu.Unlock()
		running.Go(func() { runJob(ctx, job, kick) })
	}
	slog.Info("scheduler started", "jobs", jobNames(jobs))
	return running.Wait
}

func jobNames(jobs []Job) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.Name
	}
	return out
}

func runJob(ctx context.Context, job Job, kick <-chan struct{}) {
	timer := time.NewTimer(nextWait(job.Interval))
	defer timer.Stop()
	if job.Start == AtStart {
		safeRun(ctx, job)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			safeRun(ctx, job)
			timer.Reset(nextWait(job.Interval))
		case <-kick:
			safeRun(ctx, job)
		}
	}
}

// jitterShare: a run waits interval ± interval/jitterShare/2.
const jitterShare = 10

// nextWait is the pause before a job's next run, spread at random so
// jobs started together drift apart instead of probing in one burst:
//
//	10 min → 9:30 … 10:30
func nextWait(interval time.Duration) time.Duration {
	spread := interval / jitterShare
	if spread <= 0 {
		return interval
	}
	return interval - spread/2 + rand.N(spread)
}

func safeRun(ctx context.Context, job Job) {
	started := time.Now()
	run := Run{At: started.UTC(), Every: job.Interval}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scheduler: job panicked", "job", job.Name, "recover", r)
			run.Err = "panic"
		}
		run.Duration = time.Since(started)
		runsMu.Lock()
		runs[job.Name] = run
		runsMu.Unlock()
	}()
	if err := job.Run(ctx); err != nil {
		slog.Error("scheduler: job failed", "job", job.Name, "err", err)
		run.Err = err.Error()
	}
}
