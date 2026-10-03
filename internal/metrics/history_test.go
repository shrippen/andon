package metrics_test

import (
	"fmt"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func TestTrendAndVersionEvent(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var points []metrics.Point
	for i := range 10 {
		points = append(points, metrics.Point{Day: start.AddDate(0, 0, i), Value: 0.5 + 0.01*float64(i)})
	}
	slope, last, ok := metrics.Trend(points, 5)
	if !ok || slope < 0.0099 || slope > 0.0101 || last != 0.59 {
		t.Fatalf("trend: %v %v %v", slope, last, ok)
	}
	if _, _, ok := metrics.Trend(points[:3], 5); ok {
		t.Fatal("too few points accepted")
	}

	now := time.Now()
	if _, ok := metrics.VersionEvent("Immich", "", "v1", now); ok {
		t.Fatal("first sighting is no update")
	}
	if e, ok := metrics.VersionEvent("Immich", "v1", "v2", now); !ok || e.Detail != "v1 → v2" {
		t.Fatalf("update: %+v", e)
	}
	if _, ok := metrics.VersionEvent("web", "pending:", "pending:app", now); ok {
		t.Fatal("announced image is no update")
	}
	if e, ok := metrics.VersionEvent("web", "pending:app", "pending:", now); !ok || e.Detail != "app" {
		t.Fatalf("redeploy: %+v", e)
	}
}

// TestVersionsKeepStackApart: a Komodo stack named like a service (both
// "authentik") must not share its version slot; the greeting showed
// "authentik 2026.8.3 → pending:".
func TestVersionsKeepStackApart(t *testing.T) {
	v := metrics.Read(metrics.Scope{Datasets: map[string]any{
		"authentik": &sources.AuthentikDataset{Version: "2026.8.3"},
		"komodo":    &sources.KomodoDataset{Stacks: []sources.KStack{{Name: "authentik"}}},
	}}, time.Now()).Versions
	if v["authentik"] != "2026.8.3" || len(v) != 2 {
		t.Fatalf("versions: %v", v)
	}
	for subject, version := range v {
		if e, ok := metrics.VersionEvent(subject, version+"x", version, time.Now()); ok && e.Subject != "authentik" {
			t.Fatalf("event subject %q", e.Subject)
		}
	}
}

// TestSlopeError: points on a line have no error; scattered points give a
// range around the pace.
func TestSlopeError(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var line, scattered []metrics.Point
	for i := range 10 {
		line = append(line, metrics.Point{Day: start.AddDate(0, 0, i), Value: 0.5 + 0.01*float64(i)})
		wobble := 0.005
		if i%2 == 0 {
			wobble = -wobble
		}
		scattered = append(scattered, metrics.Point{Day: start.AddDate(0, 0, i), Value: 0.5 + 0.01*float64(i) + wobble})
	}
	if se, ok := metrics.SlopeError(line); !ok || se > 1e-9 {
		t.Fatalf("line: %v %v", se, ok)
	}
	if se, ok := metrics.SlopeError(scattered); !ok || se <= 0 || se > 0.005 {
		t.Fatalf("scattered: %v %v", se, ok)
	}
}

// TestQuietHours: busy runs per weekday and hour become shares; the
// quietest run of hours skips the busy evening.
func TestQuietHours(t *testing.T) {
	day := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	h := &metrics.History{Series: map[string][]metrics.Point{}}
	for hour := range 24 {
		busy := 0.0
		if hour >= 18 || hour < 2 {
			busy = 12
		}
		h.Series[fmt.Sprintf("window.runs.3.%d", hour)] = []metrics.Point{{Day: day, Value: 12}}
		h.Series[fmt.Sprintf("window.busy.3.%d", hour)] = []metrics.Point{{Day: day, Value: busy}}
	}
	shares := metrics.BusyShares(h)
	if shares[3][20] != 1 || shares[3][3] != 0 || shares[1][3] != -1 {
		t.Fatalf("shares: %v %v %v", shares[3][20], shares[3][3], shares[1][3])
	}
	start, share, ok := metrics.QuietestHours(shares, 2)
	if !ok || share != 0 || start < 2 || start > 16 {
		t.Fatalf("quiet: %v %v %v", start, share, ok)
	}
}
