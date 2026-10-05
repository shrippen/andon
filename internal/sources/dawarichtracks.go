package sources

// Dawarich tracks: the list (GET /api/v1/tracks, GeoJSON, paged) names the
// tracks of a window; only a single track (GET /api/v1/tracks/{id}) comes
// with its transportation-mode segments. A finished track does not change,
// so its segments are kept in memory (keyed by id, revision and end) and
// each fetch only asks for new ones:
//
//	list ──► id, revision, end ──► cached? ──yes──► reuse
//	                                  └─no──► GET tracks/{id} (≤ trackReads per fetch)

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"andon/internal/drivers/services"
)

const (
	// trackReads caps the single-track reads of one fetch; the rest
	// follow on the next fetches (TracksPartial until then).
	trackReads = 300
	// trackCacheMax empties the cache when it grows beyond this many
	// tracks (several years of daily tracks).
	trackCacheMax = 20000
)

var (
	trackMu    sync.Mutex
	trackCache = map[string]DawarichTrack{}
)

// trackKey identifies one version of a track of one Dawarich instance.
func trackKey(instance string, props map[string]any) string {
	return fmt.Sprintf("%s|%d|%v|%s", instance, asInt64(props["id"]), props["revision"], asStr(props["end_at"]))
}

func cachedTrack(key string) (DawarichTrack, bool) {
	trackMu.Lock()
	defer trackMu.Unlock()
	t, ok := trackCache[key]
	return t, ok
}

func keepTrack(key string, t DawarichTrack) {
	trackMu.Lock()
	defer trackMu.Unlock()
	if len(trackCache) >= trackCacheMax {
		trackCache = map[string]DawarichTrack{}
	}
	trackCache[key] = t
}

// loadTracks reads the tracks touching [from, to] into data.
func loadTracks(ctx context.Context, api services.DawarichApi, from, to time.Time, data *DawarichDataset) {
	data.TracksFrom = from.Format(time.RFC3339)
	features, err := api.Features(ctx, "tracks", url.Values{"start_at": {from.Format(time.RFC3339)}, "end_at": {to.Format(time.RFC3339)}})
	if err != nil {
		data.TracksState = TracksFailed
		if _, ok := err.(services.ApiMissing); ok {
			data.TracksState = TracksMissing
		}
		return
	}

	data.TracksState = TracksOK
	reads := 0
	for _, f := range features {
		props := asMap(asMap(f)["properties"])
		if asInt64(props["id"]) == 0 {
			continue
		}
		key := trackKey(api.URL, props)
		if t, ok := cachedTrack(key); ok {
			data.Tracks = append(data.Tracks, t)
			continue
		}
		if reads >= trackReads {
			data.TracksState = TracksPartial
			continue
		}
		reads++

		raw, err := api.Get(ctx, "tracks/"+strconv.FormatInt(asInt64(props["id"]), 10), nil)
		if err != nil {
			if _, ok := err.(services.ApiMissing); ok {
				continue // deleted or merged in the meantime
			}
			data.TracksState = TracksPartial
			continue
		}
		t, ok := parseTrack(asList(asMap(raw)["features"]))
		if !ok {
			continue
		}
		keepTrack(key, t)
		data.Tracks = append(data.Tracks, t)
	}
	sort.Slice(data.Tracks, func(i, j int) bool { return data.Tracks[i].Start < data.Tracks[j].Start })
}

// parseTrack reads a single track (Tracks::GeojsonSerializer with
// segments): times ISO 8601, segment times Unix seconds, distances in
// metres, coordinates [lon, lat]. A track without segments (not
// classified yet) is one segment of its dominant mode.
func parseTrack(features []any) (DawarichTrack, bool) {
	if len(features) == 0 {
		return DawarichTrack{}, false
	}
	feature := asMap(features[0])
	props := asMap(feature["properties"])
	start, ok1 := unixOf(props["start_at"])
	end, ok2 := unixOf(props["end_at"])
	if !ok1 || !ok2 {
		return DawarichTrack{}, false
	}
	t := DawarichTrack{ID: asInt64(props["id"]), Start: start, End: end}

	for _, raw := range asList(props["segments"]) {
		sm := asMap(raw)
		line := asList(sm["coordinates"])
		seg := DawarichSegment{
			Start: asInt64(sm["start_time"]), End: asInt64(sm["end_time"]), Mode: asStr(sm["mode"]), Meters: asFloat(sm["distance"]),
		}
		if seg.Mode == "" || seg.End < seg.Start {
			continue
		}
		if len(line) > 0 {
			seg.FromLon, seg.FromLat = lonLat(line[0])
			seg.ToLon, seg.ToLat = lonLat(line[len(line)-1])
		}
		t.Segments = append(t.Segments, seg)
	}
	sort.Slice(t.Segments, func(i, j int) bool { return t.Segments[i].Start < t.Segments[j].Start })

	if len(t.Segments) == 0 {
		line := asList(asMap(feature["geometry"])["coordinates"])
		mode := asStr(props["dominant_mode"])
		if mode == "" {
			mode = modeUnknown
		}
		seg := DawarichSegment{Start: start, End: end, Mode: mode, Meters: asFloat(props["distance"])}
		if len(line) > 0 {
			seg.FromLon, seg.FromLat = lonLat(line[0])
			seg.ToLon, seg.ToLat = lonLat(line[len(line)-1])
		}
		t.Segments = []DawarichSegment{seg}
	}
	return t, true
}

// modeUnknown is Dawarich's mode for a segment it could not classify.
const modeUnknown = "unknown"

func lonLat(v any) (float64, float64) {
	pair := asList(v)
	if len(pair) < 2 {
		return 0, 0
	}
	return asFloat(pair[0]), asFloat(pair[1])
}

// unixOf reads an ISO 8601 time or Unix seconds.
func unixOf(v any) (int64, bool) {
	if s, ok := v.(string); ok {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return 0, false
		}
		return t.Unix(), true
	}
	n := asInt64(v)
	return n, n != 0
}

// loadDawarichPlaces reads the places Dawarich knows; an older Dawarich
// without them has none.
func loadDawarichPlaces(ctx context.Context, api services.DawarichApi) []DawarichPlace {
	raw, err := api.Get(ctx, "places", nil)
	if err != nil {
		return nil
	}
	var out []DawarichPlace
	for _, p := range asList(raw) {
		pm := asMap(p)
		out = append(out, DawarichPlace{ID: asInt64(pm["id"]), Name: asStr(pm["name"]), Lat: asFloat(pm["latitude"]), Lon: asFloat(pm["longitude"])})
	}
	return out
}
