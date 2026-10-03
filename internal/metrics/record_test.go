package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
)

// testSet is a dataset type only this test records.
type testSet struct{ IP string }

// TestRecordState: a registered recorder's state becomes a change event
// on the second, different sighting only.
func TestRecordState(t *testing.T) {
	metrics.Record(func(d *testSet, _ time.Time, r *metrics.Readings) { r.State("IPv4", d.IP) })
	now := time.Now()
	read := metrics.Read(map[string]any{"x": &testSet{IP: "5.6.7.8"}}, now)
	if len(read.States) != 1 {
		t.Fatalf("states: %v", read.States)
	}
	for subject, state := range read.States {
		if _, ok := metrics.StateEvent(subject, "", state, now); ok {
			t.Fatal("first sighting is no change")
		}
		e, ok := metrics.StateEvent(subject, "1.2.3.4", state, now)
		if !ok || e.Kind != metrics.EventChange || e.Subject != "IPv4" || e.Detail != "1.2.3.4 → 5.6.7.8" {
			t.Fatalf("change: %+v", e)
		}
	}
}
