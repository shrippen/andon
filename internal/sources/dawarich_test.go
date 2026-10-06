package sources_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/progress"
	"andon/internal/sources"
)

func TestDawarichDataNormalizesVisitsAndAreas(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/points", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"timestamp": 1750000000}]`))
	})
	mux.HandleFunc("/api/v1/areas", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 1, "name": "Kunde A", "latitude": 52.5, "longitude": 13.4, "radius": 100}]`))
	})
	mux.HandleFunc("/api/v1/visits", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{
			"id": 1, "started_at": "2026-01-05T09:00:00Z", "ended_at": "2026-01-05T11:00:00Z",
			"duration": 120, "area_id": 1, "place": {"name": "Büro", "latitude": 52.5, "longitude": 13.4}
		}]`))
	})
	mux.HandleFunc("/api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total_distance": 42}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := sources.DawarichData
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data := out.(*sources.DawarichDataset)

	if len(data.Areas) != 1 || data.Areas[0].Name != "Kunde A" {
		t.Fatalf("unexpected areas: %+v", data.Areas)
	}
	if len(data.Visits) != 1 {
		t.Fatalf("expected 1 visit, got %d", len(data.Visits))
	}
	v := data.Visits[0]
	if v.Minutes != 120 || v.Name != "Büro" || v.Lat == nil || *v.Lat != 52.5 {
		t.Fatalf("unexpected visit: %+v", v)
	}
	if data.LastPoint == "" {
		t.Fatal("expected last point to be set from the timestamp")
	}
	if data.Stats["total_distance"] != float64(42) {
		t.Fatalf("expected stats passthrough, got %+v", data.Stats)
	}
}

func TestDawarichTestReadsVersionHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Dawarich-Version", "0.24.1")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	src := sources.DawarichTest
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if m := out.(map[string]any); m["version"] != "0.24.1" {
		t.Fatalf("expected version, got %+v", m)
	}
}

// dawarichBase serves the endpoints every Dawarich fetch reads.
func dawarichBase(mux *http.ServeMux) {
	for path, body := range map[string]string{"/api/v1/points": `[]`, "/api/v1/areas": `[]`, "/api/v1/visits": `[]`, "/api/v1/stats": `{}`,
		"/api/v1/places": `[{"id": 11, "name": "Kunde Potsdam", "latitude": 52.4, "longitude": 13.06}]`} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
}

// Tracks come from the paged list, their segments from one read per
// track; a second fetch reuses the segments it read.
func TestDawarichDataReadsTracksOnce(t *testing.T) {
	mux := http.NewServeMux()
	dawarichBase(mux)
	mux.HandleFunc("/api/v1/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Total-Pages", "2")
		if r.URL.Query().Get("page") == "1" {
			w.Write([]byte(`{"type": "FeatureCollection", "features": [{"properties": {"id": 7, "revision": 0, "end_at": "2026-01-05T16:45:00Z"}}]}`))
			return
		}
		w.Write([]byte(`{"type": "FeatureCollection", "features": [{"properties": {"id": 8, "revision": 0, "end_at": "2026-01-06T10:00:00Z"}}]}`))
	})
	reads := 0
	mux.HandleFunc("/api/v1/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.PathValue("id") == "8" {
			// not classified yet: one segment of the dominant mode
			w.Write([]byte(`{"features": [{"geometry": {"type": "LineString", "coordinates": [[13.0, 52.0], [13.1, 52.1]]},
				"properties": {"id": 8, "start_at": "2026-01-06T09:00:00Z", "end_at": "2026-01-06T10:00:00Z", "distance": 9000, "dominant_mode": "cycling"}}]}`))
			return
		}
		w.Write([]byte(`{"features": [{"properties": {"id": 7, "start_at": "2026-01-05T12:00:00Z", "end_at": "2026-01-05T16:45:00Z", "segments": [
			{"mode": "stationary", "start_time": 1767625200, "end_time": 1767627300, "distance": 0, "coordinates": [[13.06, 52.4]]},
			{"mode": "driving", "start_time": 1767614400, "end_time": 1767616500, "distance": 25400, "coordinates": [[13.3, 52.5], [13.2, 52.45], [13.06, 52.4]]}
		]}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true}
	out, err := sources.DawarichData.Fetch(context.Background(), ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data := out.(*sources.DawarichDataset)

	if data.TracksState != sources.TracksOK || len(data.Tracks) != 2 {
		t.Fatalf("tracks: %s %+v", data.TracksState, data.Tracks)
	}
	first := data.Tracks[0]
	if first.ID != 7 || len(first.Segments) != 2 || first.Segments[0].Mode != "driving" {
		t.Fatalf("segments not sorted by time: %+v", first.Segments)
	}
	drive := first.Segments[0]
	if drive.Meters != 25400 || drive.FromLat != 52.5 || drive.ToLon != 13.06 {
		t.Fatalf("drive: %+v", drive)
	}
	if s := data.Tracks[1].Segments; len(s) != 1 || s[0].Mode != "cycling" || s[0].Meters != 9000 || s[0].ToLat != 52.1 {
		t.Fatalf("unclassified track: %+v", s)
	}
	if len(data.Places) != 1 || data.Places[0].Name != "Kunde Potsdam" {
		t.Fatalf("places: %+v", data.Places)
	}

	if _, err := sources.DawarichData.Fetch(context.Background(), ctx); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if reads != 2 {
		t.Fatalf("expected 2 single-track reads in total, got %d", reads)
	}
}

// A Dawarich without the tracks API keeps its visits; the tracks are
// marked missing.
func TestDawarichDataWithoutTracks(t *testing.T) {
	mux := http.NewServeMux()
	dawarichBase(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, err := sources.DawarichData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if s := out.(*sources.DawarichDataset).TracksState; s != sources.TracksMissing {
		t.Fatalf("state %q", s)
	}
}

// A position gets the name Dawarich's geocoder knows; a street gets its
// city, "Unknown Place" none.
func TestDawarichNearbyNames(t *testing.T) {
	answers := map[string]string{
		"52.5": `{"places": [{"name": "Bäckerei Kranz", "street": "Markt", "city": "Weimar"}]}`,
		"52.6": `{"places": [{"name": "Markt 3", "street": "Markt", "housenumber": "3", "city": "Weimar"}]}`,
		"52.7": `{"places": [{"name": "Unknown Place"}]}`,
		"52.8": `{"places": []}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/places/nearby" || r.URL.Query().Get("radius") != "0.15" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(answers[r.URL.Query().Get("latitude")]))
	}))
	defer srv.Close()

	for lat, want := range map[float64]string{52.5: "Bäckerei Kranz", 52.6: "Markt 3, Weimar", 52.7: "", 52.8: ""} {
		out, err := sources.DawarichNearbySource.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok",
			Params: map[string]any{"lat": lat, "lon": 11.3, "radius": 0.15}})
		if err != nil {
			t.Fatalf("%v: %v", lat, err)
		}
		if got := out.(*sources.DawarichNearby).Name; got != want {
			t.Fatalf("%v: %q, want %q", lat, got, want)
		}
	}
}

// More tracks than one fetch reads: the dataset and the maintenance
// page show how far reading is, until the next fetch reads the rest.
func TestDawarichTracksReportProgress(t *testing.T) {
	const total = 302
	mux := http.NewServeMux()
	dawarichBase(mux)
	mux.HandleFunc("/api/v1/tracks", func(w http.ResponseWriter, r *http.Request) {
		var list []string
		for id := 1; id <= total; id++ {
			list = append(list, fmt.Sprintf(`{"properties": {"id": %d, "revision": 0, "end_at": "2026-02-01T10:00:00Z"}}`, id))
		}
		w.Write([]byte(`{"type": "FeatureCollection", "features": [` + strings.Join(list, ",") + `]}`))
	})
	mux.HandleFunc("/api/v1/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"features": [{"properties": {"id": ` + r.PathValue("id") + `, "start_at": "2026-02-01T09:00:00Z", "end_at": "2026-02-01T10:00:00Z", "distance": 1000, "dominant_mode": "driving"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true}
	key := "dawarich-tracks|" + srv.URL

	out, err := sources.DawarichData.Fetch(context.Background(), ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data := out.(*sources.DawarichDataset)
	if data.TracksState != sources.TracksPartial || data.TracksRead != 300 || data.TracksTotal != total {
		t.Fatalf("first fetch: %s %d/%d", data.TracksState, data.TracksRead, data.TracksTotal)
	}
	if task, ok := progress.Get(key); !ok || task.Done != 300 || task.Total != total {
		t.Fatalf("progress: %+v %v", task, ok)
	}

	out, err = sources.DawarichData.Fetch(context.Background(), ctx)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	data = out.(*sources.DawarichDataset)
	if data.TracksState != sources.TracksOK || data.TracksRead != total {
		t.Fatalf("second fetch: %s %d/%d", data.TracksState, data.TracksRead, data.TracksTotal)
	}
	if _, ok := progress.Get(key); ok {
		t.Fatal("progress still open after all tracks were read")
	}
}
