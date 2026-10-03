package sources

// Media servers and their download helpers:
//
//	mediaserver  Jellyfin (Authorization: MediaBrowser Token=…) or Plex (X-Plex-Token)   streams, library, updates
//	arr          Sonarr or Radarr (X-Api-Key, detected)           health, queue, missing, upcoming

import (
	"context"
	"encoding/base64"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	mediaPlex        = "plex"
	mediaJellyfin    = "jellyfin"
	activeSessionsS  = "960"
	arrQueuePage     = "50"
	arrUpcomingDays  = 30 // tiles show up to this; each picks its own span
	arrWarning       = "warning"
	sonarrApp        = "Sonarr"
	plexMovieSection = "movie"
	plexShowSection  = "show"
)

// ── Jellyfin / Plex ──

// Stream is one running playback.
type Stream struct {
	User, Title string
}

type MediaServerDataset struct {
	URL            string
	Kind           string
	Version        string
	Update         bool
	Streams        []Stream
	Movies, Series int
	Episodes       int
}

var MediaServerData = source{key: "mediaserver.data", ttl: time.Minute, service: enums.ServiceMediaServer, fetch: fetchMediaServer}

func fetchMediaServer(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoMediaServer(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	var data *MediaServerDataset
	if asStr(sctx.Options["kind"]) == mediaPlex {
		data, err = plex(ctx, services.HeaderApi(sctx.URL, "X-Plex-Token", secret, sctx.TLS()))
	} else {
		// Jellyfin 10.11+ refuses the legacy X-Emby-Token header.
		data, err = jellyfin(ctx, services.HeaderApi(sctx.URL, "Authorization", `MediaBrowser Token="`+secret+`"`, sctx.TLS()))
	}
	if err != nil {
		return nil, fetchError(err)
	}
	data.URL = sctx.URL
	return data, nil
}

func jellyfin(ctx context.Context, api services.KeyedApi) (*MediaServerDataset, error) {
	info, err := api.Get(ctx, "System/Info", nil)
	if err != nil {
		return nil, err
	}
	counts, err := api.Get(ctx, "Items/Counts", nil)
	if err != nil {
		return nil, err
	}
	sessions, err := api.Get(ctx, "Sessions", url.Values{"activeWithinSeconds": {activeSessionsS}})
	if err != nil {
		return nil, err
	}
	i, c := asMap(info), asMap(counts)
	data := &MediaServerDataset{Kind: mediaJellyfin, Version: asStr(i["Version"]), Update: asBool(i["HasUpdateAvailable"]),
		Movies: int(asFloat(c["MovieCount"])), Series: int(asFloat(c["SeriesCount"])), Episodes: int(asFloat(c["EpisodeCount"]))}
	for _, raw := range asList(sessions) {
		s := asMap(raw)
		item := asMap(s["NowPlayingItem"])
		if item == nil {
			continue
		}
		data.Streams = append(data.Streams, Stream{User: asStr(s["UserName"]), Title: firstStr(asStr(item["SeriesName"]), asStr(item["Name"]))})
	}
	return data, nil
}

func plex(ctx context.Context, api services.KeyedApi) (*MediaServerDataset, error) {
	identity, err := api.Get(ctx, "identity", nil)
	if err != nil {
		return nil, err
	}
	sessions, err := api.Get(ctx, "status/sessions", nil)
	if err != nil {
		return nil, err
	}
	sections, err := api.Get(ctx, "library/sections", nil)
	if err != nil {
		return nil, err
	}
	data := &MediaServerDataset{Kind: mediaPlex, Version: asStr(asMap(asMap(identity)["MediaContainer"])["version"])}
	for _, raw := range asList(asMap(asMap(sessions)["MediaContainer"])["Metadata"]) {
		m := asMap(raw)
		data.Streams = append(data.Streams, Stream{User: asStr(asMap(m["User"])["title"]), Title: firstStr(asStr(m["grandparentTitle"]), asStr(m["title"]))})
	}
	for _, raw := range asList(asMap(asMap(sections)["MediaContainer"])["Directory"]) {
		dir := asMap(raw)
		kind := asStr(dir["type"])
		if kind != plexMovieSection && kind != plexShowSection {
			continue
		}
		// Size 0 asks only for the total.
		page, err := api.Get(ctx, "library/sections/"+url.PathEscape(asStr(dir["key"]))+"/all",
			url.Values{"X-Plex-Container-Start": {"0"}, "X-Plex-Container-Size": {"0"}})
		if err != nil {
			return nil, err
		}
		total := int(asFloat(asMap(asMap(page)["MediaContainer"])["totalSize"]))
		if kind == plexMovieSection {
			data.Movies += total
		} else {
			data.Series += total
		}
	}
	return data, nil
}

// PlayDays is the span of the play statistics.
const PlayDays = 30

// MediaPlays counts playbacks over the last PlayDays.
type MediaPlays struct {
	Daily  map[string]int // "2026-09-30" → plays
	Titles map[string]int // series or movie → plays
}

// add counts one playback.
func (p *MediaPlays) add(at time.Time, title string) {
	if at.IsZero() {
		return
	}
	p.Daily[at.Format(time.DateOnly)]++
	if title != "" {
		p.Titles[title]++
	}
}

// MediaPlaysSource reads the play history when the dialog opens: Plex's
// watch history, Jellyfin's activity log.
var MediaPlaysSource = source{key: "mediaserver.plays", ttl: detailTTL, service: enums.ServiceMediaServer, fetch: fetchMediaPlays}

// jellyfinPlaying splits an activity "selin is playing Title" (English
// server language; otherwise the whole entry is the title).
const jellyfinPlaying = " is playing "

// mediaPlaysMax caps the read history.
const mediaPlaysMax = "1000"

func fetchMediaPlays(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoMediaPlays(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().AddDate(0, 0, -PlayDays)
	out := &MediaPlays{Daily: map[string]int{}, Titles: map[string]int{}}
	if asStr(sctx.Options["kind"]) == mediaPlex {
		api := services.HeaderApi(sctx.URL, "X-Plex-Token", secret, sctx.TLS())
		hist, err := api.Get(ctx, "status/sessions/history/all", url.Values{"sort": {"viewedAt:desc"},
			"viewedAt>": {strconv.FormatInt(since.Unix(), 10)}, "X-Plex-Container-Size": {mediaPlaysMax}})
		if err != nil {
			return nil, fetchError(err)
		}
		for _, raw := range asList(asMap(asMap(hist)["MediaContainer"])["Metadata"]) {
			m := asMap(raw)
			out.add(time.Unix(asInt64(m["viewedAt"]), 0), firstStr(asStr(m["grandparentTitle"]), asStr(m["title"])))
		}
		return out, nil
	}
	api := services.HeaderApi(sctx.URL, "Authorization", `MediaBrowser Token="`+secret+`"`, sctx.TLS())
	log, err := api.Get(ctx, "System/ActivityLog/Entries", url.Values{"minDate": {since.UTC().Format(time.RFC3339)}, "limit": {mediaPlaysMax}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(asMap(log)["Items"]) {
		m := asMap(raw)
		if asStr(m["Type"]) != "VideoPlayback" && asStr(m["Type"]) != "AudioPlayback" {
			continue
		}
		name := asStr(m["Name"])
		if _, title, ok := strings.Cut(name, jellyfinPlaying); ok {
			name = title
		}
		out.add(parseTime(m["Date"]), name)
	}
	return out, nil
}

// ── Sonarr / Radarr ──

// ArrHealth is one health check message.
type ArrHealth struct {
	Level, Message string
	Source         string // the check, e.g. "IndexerStatusCheck"
}

// ArrItem is one upcoming episode or movie.
type ArrItem struct {
	Title string
	At    time.Time
	Cover string // poster path below the app: "api/v3/mediacover/12/poster-250.jpg"
}

type ArrDataset struct {
	URL      string
	App      string
	Version  string
	Health   []ArrHealth
	Queue    int
	Stuck    []string // queue items with a warning or error
	Missing  int
	Upcoming []ArrItem
}

var ArrData = source{key: "arr.data", ttl: opsTTL, service: enums.ServiceArr, fetch: fetchArr}

func fetchArr(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoArr(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadArr(ctx, services.HeaderApi(sctx.URL, "X-Api-Key", secret, sctx.TLS()), time.Now().UTC())
	if err != nil {
		return nil, fetchError(err)
	}
	data.URL = sctx.URL
	return data, nil
}

func loadArr(ctx context.Context, api services.KeyedApi, now time.Time) (*ArrDataset, error) {
	status, err := api.Get(ctx, "api/v3/system/status", nil)
	if err != nil {
		return nil, err
	}
	health, err := api.Get(ctx, "api/v3/health", nil)
	if err != nil {
		return nil, err
	}
	queue, err := api.Get(ctx, "api/v3/queue", url.Values{"pageSize": {arrQueuePage}})
	if err != nil {
		return nil, err
	}
	data := &ArrDataset{App: asStr(asMap(status)["appName"]), Version: asStr(asMap(status)["version"]),
		Queue: int(asFloat(asMap(queue)["totalRecords"]))}
	for _, raw := range asList(health) {
		h := asMap(raw)
		data.Health = append(data.Health, ArrHealth{Level: asStr(h["type"]), Message: asStr(h["message"]), Source: asStr(h["source"])})
	}
	for _, raw := range asList(asMap(queue)["records"]) {
		r := asMap(raw)
		if state := asStr(r["trackedDownloadStatus"]); state == arrWarning || state == "error" {
			data.Stuck = append(data.Stuck, asStr(r["title"]))
		}
	}
	if missing, err := api.Get(ctx, "api/v3/wanted/missing", url.Values{"pageSize": {"1"}, "monitored": {"true"}}); err == nil {
		data.Missing = int(asFloat(asMap(missing)["totalRecords"]))
	}

	data.Upcoming, err = arrUpcoming(ctx, api, data.App, now)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// arrUpcoming reads the calendar of the coming arrUpcomingDays, soonest first.
func arrUpcoming(ctx context.Context, api services.KeyedApi, app string, now time.Time) ([]ArrItem, error) {
	params := url.Values{"start": {now.Format(time.DateOnly)}, "end": {now.AddDate(0, 0, arrUpcomingDays).Format(time.DateOnly)}, "includeSeries": {"true"}}
	calendar, err := api.Get(ctx, "api/v3/calendar", params)
	if err != nil {
		return nil, err
	}
	var out []ArrItem
	for _, raw := range asList(calendar) {
		out = append(out, arrItem(asMap(raw), app, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// arrPosters is how many of the coming items the dialog shows with poster.
const arrPosters = 8

// ArrPosters maps an upcoming item's title to its poster as data: URI.
type ArrPosters struct{ ByTitle map[string]string }

// ArrPostersSource reads the posters of the coming items when the dialog opens.
var ArrPostersSource = source{key: "arr.posters", ttl: detailTTL, service: enums.ServiceArr, fetch: fetchArrPosters}

func fetchArrPosters(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return &ArrPosters{}, nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "X-Api-Key", secret, sctx.TLS())
	status, err := api.Get(ctx, "api/v3/system/status", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	items, err := arrUpcoming(ctx, api, asStr(asMap(status)["appName"]), time.Now().UTC())
	if err != nil {
		return nil, fetchError(err)
	}
	items = items[:min(len(items), arrPosters)]
	pics := make([]string, len(items))
	parallel(ctx, len(items), arrPosters, func(i int) {
		if items[i].Cover == "" {
			return
		}
		if body, kind, err := api.Bytes(ctx, items[i].Cover); err == nil && strings.HasPrefix(kind, "image/") {
			pics[i] = "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(body)
		}
	})
	out := &ArrPosters{ByTitle: map[string]string{}}
	for i, it := range items {
		if pics[i] != "" {
			out.ByTitle[it.Title] = pics[i]
		}
	}
	return out, nil
}

// arrItem names an episode "Show 2x05" or a movie by its title and next
// release (digital, disc or cinema) from today on; the calendar also lists
// films whose cinema start lies months back.
func arrItem(m map[string]any, app string, now time.Time) ArrItem {
	if app == sonarrApp {
		title := asStr(asMap(m["series"])["title"]) + " " + strconv.Itoa(int(asFloat(m["seasonNumber"]))) + "x" + pad2(int(asFloat(m["episodeNumber"])))
		return ArrItem{Title: title, At: parseTime(m["airDateUtc"]), Cover: arrCover(asInt64(m["seriesId"]))}
	}
	today := now.Truncate(24 * time.Hour)
	at := time.Time{}
	for _, key := range []string{"digitalRelease", "physicalRelease", "inCinemas"} {
		t := parseTime(m[key])
		if t.IsZero() || t.Before(today) {
			continue
		}
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	return ArrItem{Title: asStr(m["title"]), At: at, Cover: arrCover(asInt64(m["id"]))}
}

// arrCover is the small poster of a series or movie; 0 has none.
func arrCover(id int64) string {
	if id == 0 {
		return ""
	}
	return "api/v3/mediacover/" + strconv.FormatInt(id, 10) + "/poster-250.jpg"
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// ── Demo ──

// DemoMediaPlays is a month of evenings: more at weekends.
func DemoMediaPlays(now time.Time) *MediaPlays {
	var data struct {
		Titles []struct {
			Title string
			Plays int
		}
	}
	demoworld.MustDecode("library", now, &data)
	out := &MediaPlays{Daily: map[string]int{}, Titles: map[string]int{}}
	for _, t := range data.Titles {
		out.Titles[t.Title] = t.Plays
	}
	for i := range PlayDays {
		day := now.AddDate(0, 0, -i)
		n := 1 + i%3
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			n += 3
		}
		out.Daily[day.Format(time.DateOnly)] = n
	}
	return out
}

func DemoMediaServer() *MediaServerDataset {
	data := &MediaServerDataset{}
	demoworld.MustDecode("library", time.Now(), data)
	return data
}

func DemoArr(now time.Time) *ArrDataset {
	data := &ArrDataset{}
	demoworld.MustDecode("series", now, data)
	return data
}

func init() {
	Register(MediaServerData)
	Register(testOf{MediaServerData, func(d any) map[string]any { return map[string]any{"version": d.(*MediaServerDataset).Version} }})
	Register(ArrData)
	Register(ArrPostersSource)
	Register(MediaPlaysSource)
	Register(testOf{ArrData, func(d any) map[string]any {
		a := d.(*ArrDataset)
		return map[string]any{"version": a.App + " " + a.Version}
	}})
}
