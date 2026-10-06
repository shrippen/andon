package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"andon/internal/services/scheduler"
)

func TestJobRunsOnInterval(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, []scheduler.Job{{
		Name: "tick", Interval: 10 * time.Millisecond,
		Run: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil },
	}})

	time.Sleep(55 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n < 3 {
		t.Fatalf("expected at least 3 ticks in 55ms at 10ms interval, got %d", n)
	}
}

func TestFailingJobDoesNotStopOthers(t *testing.T) {
	var okCalls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, []scheduler.Job{
		{Name: "bad", Interval: 10 * time.Millisecond, Run: func(context.Context) error { return errors.New("boom") }},
		{Name: "ok", Interval: 10 * time.Millisecond, Run: func(context.Context) error { atomic.AddInt32(&okCalls, 1); return nil }},
	})

	time.Sleep(35 * time.Millisecond)
	if n := atomic.LoadInt32(&okCalls); n < 2 {
		t.Fatalf("expected the healthy job to keep running, got %d calls", n)
	}
}

func TestPanickingJobDoesNotStopOthers(t *testing.T) {
	var okCalls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, []scheduler.Job{
		{Name: "bad", Interval: 10 * time.Millisecond, Run: func(context.Context) error { panic("boom") }},
		{Name: "ok", Interval: 10 * time.Millisecond, Run: func(context.Context) error { atomic.AddInt32(&okCalls, 1); return nil }},
	})

	time.Sleep(35 * time.Millisecond)
	if n := atomic.LoadInt32(&okCalls); n < 2 {
		t.Fatalf("expected the healthy job to keep running despite the other panicking, got %d calls", n)
	}
}

func TestAtStartRunsImmediatelyAndRecordsRun(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, []scheduler.Job{{
		Name: "early", Interval: time.Hour, Start: scheduler.AtStart,
		Run: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil },
	}})

	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one run at start, got %d", calls)
	}
	if run, ok := scheduler.LastRun("early"); !ok || run.At.IsZero() || run.Err != "" {
		t.Fatalf("last run: %+v %v", run, ok)
	}
}

func TestTriggerRunsJobNow(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, []scheduler.Job{{Name: "manual", Interval: time.Hour,
		Run: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }}})
	time.Sleep(10 * time.Millisecond)

	if !scheduler.Trigger("manual") {
		t.Fatal("trigger refused")
	}
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one triggered run, got %d", calls)
	}
	if scheduler.Trigger("unknown") {
		t.Fatal("unknown job triggered")
	}
}

// TestWaitDrainsRunningJob: after stop, wait returns only once a job
// that was running has finished, so the database is not closed under it.
func TestWaitDrainsRunningJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started, finished := make(chan struct{}), make(chan struct{})
	wait := scheduler.Start(ctx, []scheduler.Job{{Name: "slow", Interval: time.Hour, Start: scheduler.AtStart,
		Run: func(context.Context) error {
			close(started)
			time.Sleep(100 * time.Millisecond)
			close(finished)
			return nil
		}}})
	<-started
	cancel()
	wait()
	select {
	case <-finished:
	default:
		t.Fatal("wait returned while the job ran")
	}
}

func TestRunningListsJobUntilItEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})

	scheduler.Start(ctx, []scheduler.Job{{
		Name: "slow", Interval: time.Hour, Start: scheduler.AtStart,
		Run: func(context.Context) error { <-release; return nil },
	}})

	deadline := time.Now().Add(time.Second)
	for !runningHas("slow") {
		if time.Now().After(deadline) {
			t.Fatal("slow job never listed as running")
		}
		time.Sleep(time.Millisecond)
	}

	close(release)
	deadline = time.Now().Add(time.Second)
	for runningHas("slow") {
		if time.Now().After(deadline) {
			t.Fatal("slow job still listed after it ended")
		}
		time.Sleep(time.Millisecond)
	}
}

func runningHas(name string) bool {
	for _, a := range scheduler.Running() {
		if a.Name == name && !a.Since.IsZero() {
			return true
		}
	}
	return false
}

func TestRecentKeepsEveryRunNewestFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32

	scheduler.Start(ctx, []scheduler.Job{{
		Name: "hist", Interval: 5 * time.Millisecond,
		Run: func(context.Context) error {
			if atomic.AddInt32(&calls, 1) == 1 {
				return errors.New("first fails")
			}
			return nil
		},
	}})
	time.Sleep(40 * time.Millisecond)
	cancel()

	var hist []scheduler.NamedRun
	for _, r := range scheduler.Recent() {
		if r.Name == "hist" {
			hist = append(hist, r)
		}
	}
	if len(hist) < 2 {
		t.Fatalf("want several runs, got %d", len(hist))
	}
	if hist[len(hist)-1].Err != "first fails" {
		t.Fatalf("oldest run = %+v, want the failed first one", hist[len(hist)-1])
	}
	for i := 1; i < len(hist); i++ {
		if hist[i].At.After(hist[i-1].At) {
			t.Fatal("runs not newest first")
		}
	}
	if len(scheduler.Recent()) > scheduler.RecentMax {
		t.Fatal("history not capped")
	}
}
