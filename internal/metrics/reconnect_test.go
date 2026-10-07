package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestRecordWAN: the box's line is a state: when it came up, or "–"
// while down; seconds of jitter between runs are no change.
func TestRecordWAN(t *testing.T) {
	since := time.Date(2026, 10, 7, 4, 2, 0, 0, time.UTC)
	read := metrics.Read(metrics.Scope{Datasets: map[string]any{"fritzbox": &sources.FritzDataset{Status: "Connected", Since: since}}}, time.Now())
	state := read.States[metrics.StateKey(metrics.SubjectWAN)]
	if state != "2026-10-07 04:02 UTC" {
		t.Fatalf("state %q", state)
	}
	down := metrics.Read(metrics.Scope{Datasets: map[string]any{"fritzbox": &sources.FritzDataset{Status: "Disconnected"}}}, time.Now())
	if got := down.States[metrics.StateKey(metrics.SubjectWAN)]; got != metrics.WANDown {
		t.Fatalf("down %q", got)
	}

	key := metrics.StateKey(metrics.SubjectWAN)
	if _, ok := metrics.StateEvent(key, "2026-10-07 04:01 UTC", state, time.Now()); ok {
		t.Fatal("a minute of jitter is no reconnect")
	}
	if _, ok := metrics.StateEvent(key, "2026-10-06 04:02 UTC", state, time.Now()); !ok {
		t.Fatal("a new connection is a change")
	}
}

// TestReconnects: each new connection is a reconnect; when a run saw the
// line down before, the outage lasted at least from then.
func TestReconnects(t *testing.T) {
	at := func(day, hour, min int) time.Time { return time.Date(2026, 9, day, hour, min, 0, 0, time.UTC) }
	change := func(when time.Time, detail string) metrics.Event {
		return metrics.Event{At: when, Kind: metrics.EventChange, Subject: metrics.SubjectWAN, Detail: detail}
	}
	h := &metrics.History{Events: []metrics.Event{ // newest first, as Load sorts them
		change(at(20, 9, 15), "– → 2026-09-20 09:12 UTC"),
		change(at(20, 8, 40), "2026-09-12 04:02 UTC → –"),
		change(at(12, 4, 5), "2026-09-11 04:02 UTC → 2026-09-12 04:02 UTC"),
		change(at(1, 4, 5), "2026-08-01 04:02 UTC → 2026-09-01 04:02 UTC"), // before the window
		{At: at(15, 1, 0), Kind: metrics.EventChange, Subject: metrics.SubjectIP, Detail: "1.2.3.4 → 5.6.7.8"},
	}}
	got := metrics.Reconnects(h, at(5, 0, 0))
	if len(got) != 2 {
		t.Fatalf("reconnects %+v", got)
	}
	if !got[0].At.Equal(at(12, 4, 2)) || got[0].Seen {
		t.Fatalf("short %+v", got[0])
	}
	if !got[1].At.Equal(at(20, 9, 12)) || !got[1].Seen || got[1].Down != 32*time.Minute {
		t.Fatalf("seen down %+v", got[1])
	}
}
