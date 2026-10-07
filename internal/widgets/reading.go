package widgets

// "reading": posts and videos of the space's news connection (Hacker
// News, Lobsters, subreddits, YouTube channels) with points, comments and
// age, and above them the followed Twitch channels that are live. The
// sites take turns, so one busy site does not fill the tile:
//
//	● elbefilmclub  Live grading: harbour at night   214 watching
//	Show HN: Timecode Clock …      HN · 214 points · 61 comments · 7 h
//	My homelab backup plan …       r/selfhosted · 912 points · 14 h

import (
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// ReadingConfig is the "reading" widget's config.
type ReadingConfig struct {
	Site  string // "" = every site
	Limit int
}

const (
	readingShown = 8
	readingAll   = "all"
)

// readingQueries: the news connection and Twitch, both from the space.
var readingQueries = []Query{
	{Name: string(enums.ServiceNews), Source: "data", Conn: ConnPeer, Service: enums.ServiceNews},
	{Name: string(enums.ServiceTwitch), Source: "data", Conn: ConnPeer, Service: enums.ServiceTwitch},
}

func init() {
	Tile[ReadingConfig]{Key: "reading", Detail: readingDetail, Category: CategoryInsight, Topic: TopicMedia, RefreshS: 30 * 60,
		Fields: []Field{sel("site", readingAll, readingAll, sources.SiteHackerNews, sources.SiteLobsters, sources.SiteReddit, sources.SiteYouTube),
			{Key: "limit", Input: InputNumber, Default: readingShown, Min: "1", Max: "30"}},
		Decode: func(r Raw) ReadingConfig {
			site := r.Pick("site")
			if site == readingAll {
				site = ""
			}
			return ReadingConfig{Site: site, Limit: r.Int("limit")}
		},
		Queries: func(ReadingConfig) []Query { return readingQueries },
		View:    readingView,
		Calm:    func(map[string]any) bool { return true }}.add()
}

// readingItems takes the sites in turns, each in its own order.
func readingItems(cfg ReadingConfig, results map[string]any) []sources.NewsItem {
	news, ok := results[string(enums.ServiceNews)].(*sources.NewsDataset)
	if !ok {
		return nil
	}
	var order []string
	bySite := map[string][]sources.NewsItem{}
	for _, it := range news.Items {
		if cfg.Site != "" && it.Site != cfg.Site {
			continue
		}
		key := it.Site + "/" + it.Feed
		if _, seen := bySite[key]; !seen {
			order = append(order, key)
		}
		bySite[key] = append(bySite[key], it)
	}

	var out []sources.NewsItem
	for round := 0; len(out) < len(news.Items); round++ {
		added := false
		for _, key := range order {
			if round < len(bySite[key]) {
				out = append(out, bySite[key][round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out
}

// liveChannels are the live Twitch channels, most watched first.
func liveChannels(results map[string]any) []sources.LiveChannel {
	tw, ok := results[string(enums.ServiceTwitch)].(*sources.TwitchDataset)
	if !ok {
		return nil
	}
	out := append([]sources.LiveChannel(nil), tw.Live...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Viewers > out[j].Viewers })
	return out
}

// ReadingLine is an item with where it comes from: a catalog key
// ("reading.site.hackernews") or a name ("r/selfhosted").
type ReadingLine struct {
	sources.NewsItem
	SourceKey, SourceName string
}

func readingView(cfg ReadingConfig, results map[string]any, _ ViewCtx) map[string]any {
	items, live := readingItems(cfg, results), liveChannels(results)
	var lines []ReadingLine
	for _, it := range firstN(items, max(cfg.Limit-len(live), 1)) {
		line := ReadingLine{NewsItem: it}
		switch src := readingSource(it).(type) {
		case string:
			line.SourceName = src
		default:
			line.SourceKey = "reading.site." + it.Site
		}
		lines = append(lines, line)
	}
	_, hasNews := results[string(enums.ServiceNews)].(*sources.NewsDataset)
	_, hasTwitch := results[string(enums.ServiceTwitch)].(*sources.TwitchDataset)
	return map[string]any{"Lines": lines, "Live": live, "Any": hasNews || hasTwitch}
}

func readingDetail(cfg ReadingConfig, results map[string]any, _ ViewCtx) DetailView {
	items, live := readingItems(cfg, results), liveChannels(results)
	body := &DetailBody{Facts: []Kpi{{Value: len(items), Label: T("reading.items")}, {Value: len(live), Label: T("reading.live")}}}

	if len(live) > 0 {
		var rows [][]Cell
		for _, c := range live {
			rows = append(rows, []Cell{{Value: c.User, Href: c.URL}, {Value: c.Title}, {Value: orNone(c.Game)}, {Value: c.Viewers}, {Value: agoOf(c.Since)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("reading.live_now"), Data: Table{
			Head: []Text{T("reading.col.channel"), T("reading.col.title"), T("reading.col.game"), T("reading.col.viewers"), T("reading.col.since")}, Rows: rows, Num: []int{3}}})
	}
	if len(items) > 0 {
		var rows [][]Cell
		for _, it := range items {
			points, comments := Cell{Value: it.Points}, Cell{Value: it.Comments, Href: it.Link}
			if it.Site == sources.SiteYouTube {
				points, comments = Cell{Value: "–"}, Cell{Value: "–"} // a feed tells views, not votes
			}
			rows = append(rows, []Cell{{Value: it.Title, Href: it.URL}, {Value: readingSource(it)}, points, comments, {Value: agoOf(it.At)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("reading.list"), Data: Table{
			Head: []Text{T("reading.col.title"), T("reading.col.site"), T("reading.col.points"), T("reading.col.comments"), T("reading.col.age")}, Rows: rows, Num: []int{2, 3}}})
	}
	if news, ok := results[string(enums.ServiceNews)].(*sources.NewsDataset); ok && len(news.Failed) > 0 {
		body.Facts = append(body.Facts, Kpi{Value: strings.Join(news.Failed, ", "), Label: T("reading.failed"), Tier: "yellow"})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// readingSource names where an item comes from: "Hacker News",
// "r/selfhosted", the channel.
func readingSource(it sources.NewsItem) any {
	switch {
	case it.Site == sources.SiteReddit:
		return "r/" + it.Feed
	case it.Feed != "":
		return it.Feed
	}
	return Txt("reading.site." + it.Site)
}
