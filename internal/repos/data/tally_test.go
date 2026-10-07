package data_test

import (
	"testing"

	"andon/internal/repos/data"
)

// TestAddSamplesCounts: added values sum up within a day, unlike
// PutSamples, where the last run wins.
func TestAddSamplesCounts(t *testing.T) {
	q := openTestDB(t)
	sp := spaceID(t, q)
	for _, up := range []float64{1, 0, 1} {
		if err := data.AddSamples(q, sp, 0, "2026-09-27", map[string]float64{"kuma.runs.nas": 1, "kuma.up.nas": up}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := data.SamplesSince(q, sp, 0, "2026-09-27")
	if err != nil {
		t.Fatal(err)
	}
	if got["kuma.runs.nas"][0].Value != 3 || got["kuma.up.nas"][0].Value != 2 {
		t.Fatalf("tallies: %+v", got)
	}
}

// TestLowSamples: the day keeps its lowest value, e.g. a UPS's shortest
// runtime over the day's runs.
func TestLowSamples(t *testing.T) {
	q := openTestDB(t)
	sp := spaceID(t, q)
	for _, v := range []float64{1260, 840, 1100} {
		if err := data.LowSamples(q, sp, 0, "2026-09-27", map[string]float64{"ups.runtime.nas": v}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := data.SamplesSince(q, sp, 0, "2026-09-27")
	if err != nil {
		t.Fatal(err)
	}
	if got["ups.runtime.nas"][0].Value != 840 {
		t.Fatalf("low: %+v", got)
	}
}
