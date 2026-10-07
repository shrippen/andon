package widgets

// "now_playing": what plays right now across the media server and its
// helpers (sources.StreamSource: Jellyfin/Plex, Tautulli, Navidrome,
// Audiobookshelf), with Seerr's open requests; the dialog adds the most
// active users and libraries of 30 days (Jellystat).

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

// NowPlayingConfig is the "now_playing" widget's config.
type NowPlayingConfig struct{}

var playServices = []enums.ServiceType{enums.ServiceMediaServer, enums.ServiceTautulli, enums.ServiceNavidrome, enums.ServiceAudiobookshelf,
	enums.ServiceJellystat, enums.ServiceSeerr}

func init() {
	Tile[NowPlayingConfig]{Key: "now_playing", Detail: nowPlayingDetail, Category: CategoryInsight, Topic: TopicMedia, RefreshS: 60,
		Decode: func(Raw) NowPlayingConfig { return NowPlayingConfig{} },
		Queries: func(NowPlayingConfig) []Query {
			var out []Query
			for _, s := range playServices {
				out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
			}
			return out
		},
		View: nowPlayingView}.add()
}

// PlayLine is a stream with the service it plays on.
type PlayLine struct {
	sources.Stream
	Service enums.ServiceType
}

func nowStreams(results map[string]any) []PlayLine {
	var out []PlayLine
	for _, s := range playServices {
		if src, ok := results[string(s)].(sources.StreamSource); ok {
			for _, st := range src.NowStreams() {
				out = append(out, PlayLine{Stream: st, Service: s})
			}
		}
	}
	return out
}

func nowPlayingView(_ NowPlayingConfig, results map[string]any, _ ViewCtx) map[string]any {
	out := map[string]any{"Streams": nowStreams(results)}
	if seerr, ok := results[string(enums.ServiceSeerr)].(*sources.SeerrDataset); ok {
		out["Requests"], out["Stuck"] = seerr.Pending+seerr.Approved+seerr.Processing, len(seerr.Stuck)
	}
	return out
}

func nowPlayingDetail(_ NowPlayingConfig, results map[string]any, _ ViewCtx) DetailView {
	body := &DetailBody{}
	var rows [][]Cell
	for _, p := range nowStreams(results) {
		rows = append(rows, []Cell{{Value: p.Title}, {Value: orNone(p.User)}, {Value: Txt("service." + string(p.Service))}})
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("playing.now"), Data: Table{
			Head: []Text{T("playing.col.title"), T("playing.col.user"), T("playing.col.service")}, Rows: rows}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("playing.nothing")})
	}
	if stats, ok := results[string(enums.ServiceJellystat)].(*sources.PlayDataset); ok {
		for _, list := range []struct {
			label string
			stats []sources.PlayStat
		}{{"playing.top_users", stats.Top}, {"playing.top_libraries", stats.Libraries}} {
			if len(list.stats) == 0 {
				continue
			}
			top := 1
			for _, s := range list.stats {
				top = max(top, s.Plays)
			}
			var bars []ShareBar
			for _, s := range list.stats {
				bars = append(bars, ShareBar{Name: s.Name, Pct: float64(s.Plays) / float64(top) * percentScale, Value: TxtA("playing.plays", "n", s.Plays)})
			}
			body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T(list.label), Data: bars})
		}
	}
	if seerr, ok := results[string(enums.ServiceSeerr)].(*sources.SeerrDataset); ok {
		body.Side = append(body.Side, Fact{Label: T("playing.pending"), Value: seerr.Pending}, Fact{Label: T("playing.approved"), Value: seerr.Approved},
			Fact{Label: T("playing.processing"), Value: seerr.Processing}, Fact{Label: T("playing.available"), Value: seerr.Available})
		if len(seerr.Stuck) > 0 {
			var stuck [][]Cell
			for _, r := range seerr.Stuck {
				stuck = append(stuck, []Cell{{Value: r.Title}, {Value: orNone(r.By)}, {Value: agoOf(r.Requested), State: "warn"}})
			}
			body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("playing.stuck"), Data: Table{
				Head: []Text{T("playing.col.title"), T("playing.col.by"), T("playing.col.requested")}, Rows: stuck}})
		}
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
