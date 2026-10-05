package svcdata

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// paceAt is a fixed now for the pace tests.
var paceAt = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// TestPaceDefault: without rate information the service's interval holds.
func TestPaceDefault(t *testing.T) {
	p := &pacer{states: map[paceKey]Pace{}}
	next := p.next(paceKey{conn: 1}, paceRule{base: 5 * time.Minute}, sources.Usage{Calls: 3}, fetchOK, paceAt)
	if !next.Equal(paceAt.Add(5*time.Minute)) || p.states[paceKey{conn: 1}].Reason != PaceDefault {
		t.Fatalf("next %v %+v", next, p.states)
	}
}

// TestPaceStretchesForQuota: what is left above the reserve must last
// until the quota renews: 500 calls a fetch, 3500 to spend in an hour
// → every 60·500/3500 ≈ 8.6 minutes instead of 5.
func TestPaceStretchesForQuota(t *testing.T) {
	p := &pacer{states: map[paceKey]Pace{}}
	u := sources.Usage{Calls: 500, Limit: 5000, Remaining: 4500, Reset: paceAt.Add(time.Hour)}
	next := p.next(paceKey{conn: 1}, paceRule{base: 5 * time.Minute}, u, fetchOK, paceAt)
	if got := next.Sub(paceAt); got < 8*time.Minute || got > 9*time.Minute || p.states[paceKey{conn: 1}].Reason != PaceTight {
		t.Fatalf("every %v", got)
	}

	// Within the reserve: wait for the renewal.
	u.Remaining = 900
	if next := p.next(paceKey{conn: 1}, paceRule{base: 5 * time.Minute}, u, fetchOK, paceAt); !next.Equal(u.Reset) {
		t.Fatalf("reserve: %v", next)
	}

	// A fixed interval is kept.
	if next := p.next(paceKey{conn: 2}, paceRule{base: 5 * time.Minute, manual: true}, u, fetchOK, paceAt); !next.Equal(paceAt.Add(5 * time.Minute)) {
		t.Fatalf("manual: %v", next)
	}
}

// TestPaceRefused: a 429 waits for Retry-After; without one the interval
// doubles per refusal up to paceMaxBackoff, also for a fixed interval.
func TestPaceRefused(t *testing.T) {
	p := &pacer{states: map[paceKey]Pace{}}
	k, rule := paceKey{conn: 1}, paceRule{base: 5 * time.Minute, manual: true}
	retry := sources.Usage{Calls: 1, Limited: true, RetryAt: paceAt.Add(20 * time.Minute)}
	if next := p.next(k, rule, retry, fetchFailed, paceAt); !next.Equal(retry.RetryAt) || p.states[k].Reason != PaceRefused {
		t.Fatalf("retry-after: %v", next)
	}

	bare := sources.Usage{Calls: 1, Limited: true}
	var waits []time.Duration
	for range 8 {
		waits = append(waits, p.next(k, rule, bare, fetchFailed, paceAt).Sub(paceAt))
	}
	if waits[0] != 40*time.Minute || waits[1] != 80*time.Minute || waits[7] != paceMaxBackoff {
		t.Fatalf("backoff %v", waits)
	}

	// Answered again: back to the interval.
	if next := p.next(k, rule, sources.Usage{Calls: 1}, fetchOK, paceAt); !next.Equal(paceAt.Add(5 * time.Minute)) {
		t.Fatalf("recovered: %v", next)
	}
}

// TestPaceError: another failure retries soon, as before.
func TestPaceError(t *testing.T) {
	p := &pacer{states: map[paceKey]Pace{}}
	if next := p.next(paceKey{conn: 1}, paceRule{base: time.Hour}, sources.Usage{Calls: 1}, fetchFailed, paceAt); !next.Equal(paceAt.Add(errorTTL)) {
		t.Fatalf("error: %v", next)
	}
}

// TestPaceOf: a connection shows a held back query first, else its main
// one, never a side query's long default.
func TestPaceOf(t *testing.T) {
	p := &pacer{states: map[paceKey]Pace{
		{conn: 1, query: "a", main: true}: {Every: 5 * time.Minute, Reason: PaceDefault},
		{conn: 1, query: "b"}:             {Every: 6 * time.Hour, Reason: PaceDefault},
		{conn: 2, query: "a"}:             {Every: 6 * time.Hour, Reason: PaceRefused},
	}}
	if got := p.of(1); got.Every != 5*time.Minute {
		t.Fatalf("main: %+v", got)
	}
	p.states[paceKey{conn: 1, query: "c"}] = Pace{Every: 2 * time.Hour, Reason: PaceTight}
	if got := p.of(1); got.Reason != PaceTight || got.Every != 2*time.Hour {
		t.Fatalf("tight: %+v", got)
	}
}
