package sources

// Media helpers around the media server:
//
//	Tautulli        GET api/v2?apikey=…&cmd=get_activity → {response: {data: {sessions: [{friendly_name, full_title, state}], total_bandwidth}}}
//	Jellystat       POST stats/getMostActiveUsers {days}, stats/getMostViewedLibraries {days} (x-api-token)
//	Navidrome       Subsonic API: rest/getNowPlaying, rest/getScanStatus (u, t = md5(password+salt), s)
//	Audiobookshelf  GET api/users/online (Bearer) → {usersOnline: [{username, session: {displayTitle}}]}; GET api/libraries
//	Seerr           GET api/v1/request/count (X-Api-Key) → {pending, approved, processing, available};
//	                GET api/v1/request?filter=processing → results[{status, media{status}, createdAt, requestedBy{displayName}}]
//
// Every one that knows what plays now is a StreamSource, so the update
// window and the busy hours see all of them.

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// StreamSource is a dataset that knows what plays right now.
type StreamSource interface{ NowStreams() []Stream }

// NowStreams are the media server's streams.
func (d *MediaServerDataset) NowStreams() []Stream { return d.Streams }

// PlayDataset is what a media helper reports: streams now, counts.
type PlayDataset struct {
	URL       string
	Tool      enums.ServiceType
	Streams   []Stream
	Bandwidth int        // kbit/s of all streams (Tautulli), 0 unknown
	Top       []PlayStat // most active users, then libraries (Jellystat)
	Libraries []PlayStat
	Items     int  // songs (Navidrome), libraries (Audiobookshelf)
	Scanning  bool // a library scan runs (Navidrome)
}

// PlayStat is a name with its plays.
type PlayStat struct {
	Name  string
	Plays int
}

func (d *PlayDataset) NowStreams() []Stream { return d.Streams }

// SeerrRequest is a request that waits too long.
type SeerrRequest struct {
	Title, Kind, By string
	Requested       time.Time
}

// SeerrDataset is Jellyseerr's or Overseerr's requests.
type SeerrDataset struct {
	URL                                      string
	Pending, Approved, Processing, Available int
	Stuck                                    []SeerrRequest // approved, still not available after days
}

var (
	TautulliData       = source{key: "tautulli.data", ttl: time.Minute, service: enums.ServiceTautulli, fetch: fetchTautulli}
	JellystatData      = source{key: "jellystat.data", ttl: opsTTL, service: enums.ServiceJellystat, fetch: fetchJellystat}
	NavidromeData      = source{key: "navidrome.data", ttl: time.Minute, service: enums.ServiceNavidrome, fetch: fetchNavidrome}
	AudiobookshelfData = source{key: "audiobookshelf.data", ttl: time.Minute, service: enums.ServiceAudiobookshelf, fetch: fetchAudiobookshelf}
	SeerrData          = source{key: "seerr.data", ttl: opsTTL, service: enums.ServiceSeerr, fetch: fetchSeerr}
)

// playDays is the window of Jellystat's statistics.
const playDays = 30

func fetchTautulli(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPlay(time.Now().UTC(), enums.ServiceTautulli), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}
	raw, err := api.Get(ctx, "api/v2", url.Values{"apikey": {secret}, "cmd": {"get_activity"}})
	if err != nil {
		return nil, fetchError(err)
	}
	act := asMap(asMap(asMap(raw)["response"])["data"])
	data := &PlayDataset{URL: sctx.URL, Tool: enums.ServiceTautulli, Bandwidth: int(asFloat(act["total_bandwidth"]))}
	for _, s := range asList(act["sessions"]) {
		m := asMap(s)
		data.Streams = append(data.Streams, Stream{User: asStr(m["friendly_name"]), Title: asStr(m["full_title"])})
	}
	return data, nil
}

func fetchJellystat(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPlay(time.Now().UTC(), enums.ServiceJellystat), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "x-api-token", secret, sctx.TLS())
	data := &PlayDataset{URL: sctx.URL, Tool: enums.ServiceJellystat}
	users, err := api.Post(ctx, "stats/getMostActiveUsers", map[string]any{"days": playDays})
	if err != nil {
		return nil, fetchError(err)
	}
	data.Top = playStats(users)
	if libs, err := api.Post(ctx, "stats/getMostViewedLibraries", map[string]any{"days": playDays}); err == nil {
		data.Libraries = playStats(libs)
	}
	return data, nil
}

// playStats reads Jellystat's lists tolerantly: a name (UserName, Name)
// and a count (Plays, TotalPlays).
func playStats(raw any) []PlayStat {
	var out []PlayStat
	for _, item := range asList(raw) {
		m := asMap(item)
		name := asStr(m["UserName"])
		if name == "" {
			name = asStr(m["Name"])
		}
		plays := asFloat(m["Plays"])
		if plays == 0 {
			plays = asFloat(m["TotalPlays"])
		}
		if name != "" {
			out = append(out, PlayStat{Name: name, Plays: int(plays)})
		}
	}
	return out
}

// subsonicVersion is the Subsonic API version Andon speaks.
const subsonicVersion = "1.16.1"

func fetchNavidrome(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPlay(time.Now().UTC(), enums.ServiceNavidrome), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, pass, _ := strings.Cut(secret, ":")
	saltBytes := make([]byte, 8)
	_, _ = rand.Read(saltBytes)
	salt := hex.EncodeToString(saltBytes)
	sum := md5.Sum([]byte(pass + salt))
	auth := url.Values{"u": {user}, "t": {hex.EncodeToString(sum[:])}, "s": {salt}, "v": {subsonicVersion}, "c": {"andon"}, "f": {"json"}}
	api := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}

	raw, err := api.Get(ctx, "rest/getNowPlaying", auth)
	if err != nil {
		return nil, fetchError(err)
	}
	resp := asMap(asMap(raw)["subsonic-response"])
	if asStr(resp["status"]) != "ok" {
		return nil, newSourceError("navidrome.refused")
	}
	data := &PlayDataset{URL: sctx.URL, Tool: enums.ServiceNavidrome}
	for _, e := range asList(asMap(resp["nowPlaying"])["entry"]) {
		m := asMap(e)
		data.Streams = append(data.Streams, Stream{User: asStr(m["username"]), Title: strings.TrimSpace(asStr(m["artist"]) + " – " + asStr(m["title"]))})
	}
	if scan, err := api.Get(ctx, "rest/getScanStatus", auth); err == nil {
		st := asMap(asMap(asMap(scan)["subsonic-response"])["scanStatus"])
		data.Scanning, _ = st["scanning"].(bool)
		data.Items = int(asFloat(st["count"]))
	}
	return data, nil
}

func fetchAudiobookshelf(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPlay(time.Now().UTC(), enums.ServiceAudiobookshelf), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	raw, err := api.Get(ctx, "api/users/online", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &PlayDataset{URL: sctx.URL, Tool: enums.ServiceAudiobookshelf}
	for _, u := range asList(asMap(raw)["usersOnline"]) {
		m := asMap(u)
		if title := asStr(asMap(m["session"])["displayTitle"]); title != "" {
			data.Streams = append(data.Streams, Stream{User: asStr(m["username"]), Title: title})
		}
	}
	if libs, err := api.Get(ctx, "api/libraries", nil); err == nil {
		data.Items = len(asList(asMap(libs)["libraries"]))
	}
	return data, nil
}

// seerrApproved and seerrWaiting are Seerr's request status "approved"
// and the media states that are not available yet (pending, processing).
const seerrApproved = 2

var seerrWaiting = map[int]bool{2: true, 3: true}

// seerrStuckAfter is how long an approved request may take.
const seerrStuckAfter = 3 * 24 * time.Hour

func fetchSeerr(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoSeerr(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "X-Api-Key", secret, sctx.TLS())
	raw, err := api.Get(ctx, "api/v1/request/count", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	c := asMap(raw)
	data := &SeerrDataset{URL: sctx.URL, Pending: int(asFloat(c["pending"])), Approved: int(asFloat(c["approved"])),
		Processing: int(asFloat(c["processing"])), Available: int(asFloat(c["available"]))}
	list, err := api.Get(ctx, "api/v1/request", url.Values{"filter": {"processing"}, "take": {"50"}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, item := range asList(asMap(list)["results"]) {
		r := asMap(item)
		media := asMap(r["media"])
		created := parseTime(r["createdAt"])
		if int(asFloat(r["status"])) != seerrApproved || !seerrWaiting[int(asFloat(media["status"]))] || time.Since(created) < seerrStuckAfter {
			continue
		}
		title := asStr(media["title"]) // set by newer Seerr; else the TMDB id
		if title == "" {
			title = "TMDB " + strconv.FormatInt(int64(asFloat(media["tmdbId"])), 10)
		}
		data.Stuck = append(data.Stuck, SeerrRequest{Title: title, Kind: asStr(r["type"]), By: asStr(asMap(r["requestedBy"])["displayName"]), Requested: created})
	}
	return data, nil
}

// DemoPlay is one media helper of the studio.
func DemoPlay(now time.Time, tool enums.ServiceType) *PlayDataset {
	data := &PlayDataset{Tool: tool}
	var p struct {
		URL                          string
		Bandwidth, Songs             int
		Libraries                    any
		Scanning                     bool
		Sessions, NowPlaying, Online []struct{ User, Title, Artist string }
		Users                        []PlayStat
	}
	demoworld.MustDecode("listening."+string(tool), now, &p)
	data.URL, data.Bandwidth, data.Scanning, data.Top = p.URL, p.Bandwidth, p.Scanning, p.Users
	for _, s := range append(append(p.Sessions, p.NowPlaying...), p.Online...) {
		title := s.Title
		if s.Artist != "" {
			title = s.Artist + " – " + s.Title
		}
		data.Streams = append(data.Streams, Stream{User: s.User, Title: title})
	}
	switch libs := p.Libraries.(type) {
	case float64:
		data.Items = int(libs)
	case []any:
		for _, l := range libs {
			m := asMap(l)
			data.Libraries = append(data.Libraries, PlayStat{Name: asStr(m["name"]), Plays: int(asFloat(m["plays"]))})
		}
	}
	if p.Songs > 0 {
		data.Items = p.Songs
	}
	return data
}

func DemoSeerr(now time.Time) *SeerrDataset {
	var p struct {
		SeerrDataset
		Stuck []struct {
			Title, Type, By string
			Requested       time.Time
		}
	}
	demoworld.MustDecode("listening.seerr", now, &p)
	data := p.SeerrDataset
	data.Stuck = nil
	for _, s := range p.Stuck {
		data.Stuck = append(data.Stuck, SeerrRequest{Title: s.Title, Kind: s.Type, By: s.By, Requested: s.Requested})
	}
	return &data
}

func init() {
	for _, s := range []source{TautulliData, JellystatData, NavidromeData, AudiobookshelfData} {
		Register(s)
		Register(testOf{s, func(d any) map[string]any { return map[string]any{"streams": len(d.(*PlayDataset).Streams)} }})
	}
	Register(SeerrData)
	Register(testOf{SeerrData, func(d any) map[string]any { return map[string]any{"stuck": len(d.(*SeerrDataset).Stuck)} }})
}
