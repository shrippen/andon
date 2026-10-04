package widgets

// Detail dialogs of the media tiles: Sonarr/Radarr, FreshRSS, Linkwarden,
// Jellyfin/Plex, SABnzbd, feeds and pictures.

import (
	"cmp"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	arrDetailDays  = 14
	mediaListLimit = 20
)

// arrDetail: health, queue, the coming weeks' calendar.
func arrDetail(cfg ArrConfig, data *sources.ArrDataset, _ ViewCtx, results map[string]any) DetailView {
	until := time.Now().AddDate(0, 0, max(cfg.Days, arrDetailDays))
	var events []Event
	for _, it := range data.Upcoming {
		if it.At.Before(until) {
			events = append(events, Event{At: it.At, Title: it.Title, State: it.At.Format(timeOfDay), Tier: "cyan"})
		}
	}
	var health []LitRow
	for _, h := range data.Health {
		health = append(health, LitRow{Name: h.Message, Meta: h.Source, State: alertState(h.Level)})
	}
	var stuck []LitRow
	for _, s := range data.Stuck {
		stuck = append(stuck, LitRow{Name: s, State: "warn"})
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.arr.app"), Value: data.App + " " + data.Version}, {Label: T("detail.arr.queue"), Value: data.Queue},
			{Label: T("detail.arr.stuck"), Value: len(data.Stuck), State: stateIf(len(data.Stuck) > 0, "warn")}, {Label: T("detail.arr.missing"), Value: data.Missing}},
		Facts: []Kpi{{Value: len(events), Label: arrInDays(max(cfg.Days, arrDetailDays))}, {Value: data.Queue, Label: T("detail.arr.queue")},
			{Value: len(data.Stuck), Label: T("detail.arr.stuck"), Tier: tierIf(len(data.Stuck) > 0, "yellow", "")}, {Value: data.Missing, Label: T("detail.arr.missing")}},
	}
	if len(events) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.arr.upcoming"), Data: events})
	}

	// Posters of the next items (fetched on open).
	if posters, ok := results[openName].(*sources.ArrPosters); ok {
		var pics []Image
		for _, it := range data.Upcoming {
			if uri := posters.ByTitle[it.Title]; uri != "" {
				pics = append(pics, Image{DataURI: uri, Alt: it.Title, Caption: it.Title})
			}
		}
		if len(pics) > 0 {
			body.Blocks = append(body.Blocks, Block{Kind: BlockThumbs, Label: T("detail.arr.posters"), Data: pics})
		}
	}
	pair := []Block{}
	if len(health) > 0 {
		pair = append(pair, Block{Kind: BlockRows, Label: T("detail.arr.health"), Data: health})
	}
	if len(stuck) > 0 {
		pair = append(pair, Block{Kind: BlockRows, Label: T("detail.arr.stuck_items"), Data: stuck})
	}
	body.Blocks = append(body.Blocks, pairOf(pair)...)
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.arr.healthy", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if len(data.Health) > 0 {
		head.State, head.StateKey = "warn", "detail.arr.warnings"
	}
	return DetailView{Head: head, Body: body}
}

// playBlocks: plays per day over the last month, the most played titles.
func playBlocks(p *sources.MediaPlays) []Block {
	now := time.Now()
	days := make([]float64, sources.PlayDays)
	total := 0
	for i := range days {
		n := p.Daily[now.AddDate(0, 0, i-sources.PlayDays+1).Format(time.DateOnly)]
		days[i], total = float64(n), total+n
	}
	if total == 0 {
		return nil
	}
	g := ColGraph(days, "s1")
	g.Ticks = spanTicks(now, sources.PlayDays)
	out := []Block{{Kind: BlockGraph, Label: T("detail.media.plays"), Meta: total, Data: g}}
	titles := slices.Collect(maps.Keys(p.Titles))
	slices.SortFunc(titles, func(a, b string) int { return cmp.Or(cmp.Compare(p.Titles[b], p.Titles[a]), cmp.Compare(a, b)) })
	var bars []ShareBar
	for _, t := range firstN(titles, mediaListLimit) {
		bars = append(bars, ShareBar{Name: t, Pct: float64(p.Titles[t]) * percentScale / float64(p.Titles[titles[0]]), Value: p.Titles[t]})
	}
	if len(bars) > 0 {
		out = append(out, Block{Kind: BlockBars, Label: T("detail.media.top"), Data: bars})
	}
	return out
}

// arrInDays labels the upcoming count with its span.
func arrInDays(days int) Text { return textArgs("detail.arr.in_days", "n", days) }

// freshrssDetail: unread per feed and category, quiet feeds.
func freshrssDetail(cfg FreshRSSConfig, data *sources.FreshRSSDataset, ctx ViewCtx, results map[string]any) DetailView {
	feeds := append([]sources.Feed(nil), data.Feeds...)
	sort.Slice(feeds, func(a, b int) bool { return feeds[a].Unread > feeds[b].Unread })
	byCat := map[string]int{}
	quiet := 0
	quietDays := rules.Setting(ctx.Settings, "freshrss.stale_feed", "days")
	var rows [][]Cell
	for _, f := range feeds {
		byCat[f.Category] += f.Unread
		isQuiet := metrics.FeedSilent(f, time.Now(), quietDays)
		if isQuiet {
			quiet++
		}
		rows = append(rows, []Cell{{Value: f.Title}, {Value: f.Category}, {Value: f.Unread}, {Value: agoOf(f.Newest), State: stateIf(isQuiet, "warn")}})
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(a, b int) bool { return byCat[cats[a]] > byCat[cats[b]] })
	var bars []ShareBar
	for _, c := range cats {
		bars = append(bars, ShareBar{Name: c, Pct: float64(byCat[c]) * percentScale / float64(max(byCat[cats[0]], 1)), Value: byCat[c]})
	}
	body := &DetailBody{
		Side:  []Fact{{Label: T("detail.rss.feeds"), Value: len(feeds)}, {Label: T("detail.rss.unread"), Value: data.Unread}, {Label: textArgs("detail.rss.quiet", "n", int(quietDays)), Value: quiet, State: stateIf(quiet > 0, "warn")}},
		Facts: []Kpi{{Value: data.Unread, Label: T("detail.rss.unread"), Tier: tierIf(data.Unread > 0, "yellow", "")}, {Value: quiet, Label: textArgs("detail.rss.quiet", "n", int(quietDays))}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.rss.by_category"), Data: bars},
			{Kind: BlockTable, Label: T("detail.rss.list"), Data: Table{Head: []Text{T("detail.rss.feed"), T("detail.rss.category"), T("detail.rss.unread"), T("detail.rss.newest")}, Rows: firstN(rows, mediaListLimit), Num: []int{2}}}},
	}
	// Unread per day: does the pile grow?
	now := time.Now()
	if unread := dailySeries(historyOf(results), metrics.SampleKey("freshrss", "unread"), now, historyDetailDays); hasValues(unread) {
		g := LineGraph(Series{Values: unread, Class: "s1"})
		g.Ticks = spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.rss.unread_days"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// linkwardenDetail: collections and the newest links.
func linkwardenDetail(cfg LinkwardenConfig, data *sources.LinkwardenDataset, _ ViewCtx, results map[string]any) DetailView {
	counts := map[string]int{}
	links := append([]sources.Bookmark(nil), data.Links...)
	for _, l := range links {
		counts[l.Collection]++
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(a, b int) bool { return counts[names[a]] > counts[names[b]] })
	var bars []ShareBar
	for _, n := range names {
		bars = append(bars, ShareBar{Name: n, Pct: float64(counts[n]) * percentScale / float64(max(counts[names[0]], 1)), Value: counts[n]})
	}
	sort.SliceStable(links, func(a, b int) bool { return links[a].Created.After(links[b].Created) })
	var rows [][]Cell
	for _, l := range firstN(links, mediaListLimit) {
		rows = append(rows, []Cell{{Value: dayOf(l.Created)}, {Value: l.Name}, {Value: l.Collection}})
	}
	body := &DetailBody{
		Side:  []Fact{{Label: T("detail.linkwarden.links"), Value: len(links)}, {Label: T("detail.linkwarden.collections"), Value: len(names)}},
		Facts: []Kpi{{Value: len(links), Label: T("detail.linkwarden.links")}, {Value: len(names), Label: T("detail.linkwarden.collections")}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.linkwarden.by_collection"), Data: bars},
			{Kind: BlockTable, Label: T("detail.linkwarden.newest"), Data: Table{Head: []Text{T("detail.linkwarden.saved"), T("detail.linkwarden.name"), T("detail.linkwarden.collection")}, Rows: rows}}},
	}
	if pre, ok := results[openName].(*sources.LinkwardenPreviews); ok && len(pre.List) > 0 {
		var pics []Image
		for _, t := range pre.List {
			pics = append(pics, Image{DataURI: t.DataURI, Alt: t.Title, Caption: t.Title})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockThumbs, Label: T("detail.linkwarden.previews"), Data: pics})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// mediaDetail: streams now, the library, the version.
func mediaDetail(cfg MediaConfig, results map[string]any, _ ViewCtx) DetailView {
	data, ok := results["data"].(*sources.MediaServerDataset)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	var streams [][]Cell
	for _, s := range data.Streams {
		who := s.User
		if !cfg.Users {
			who = "–"
		}
		streams = append(streams, []Cell{{Value: who}, {Value: s.Title}})
	}
	version := data.Version
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.media.server"), Value: data.Kind + " " + version, State: stateIf(data.Update, "warn")}, {Label: T("detail.media.movies"), Value: data.Movies},
			{Label: T("detail.media.series"), Value: data.Series}, {Label: T("detail.media.episodes"), Value: data.Episodes}},
		Facts: []Kpi{{Value: len(data.Streams), Label: T("detail.media.streams"), Tier: tierIf(len(data.Streams) > 0, "cyan", "")}, {Value: data.Movies, Label: T("detail.media.movies")},
			{Value: data.Episodes, Label: T("detail.media.episodes")}},
	}
	if len(streams) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.media.now"), Data: Table{Head: []Text{T("detail.media.user"), T("detail.media.title")}, Rows: streams}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.media.idle")})
	}
	if plays, ok := results[openName].(*sources.MediaPlays); ok {
		body.Blocks = append(body.Blocks, playBlocks(plays)...)
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.immich.current", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if data.Update {
		head.State, head.StateKey = "warn", "detail.update_available"
	}
	return DetailView{Head: head, Body: body}
}

// sabDetail (record without facts column): speed, disk, queue, failures.
func sabDetail(cfg SabConfig, data *sources.SabnzbdDataset, _ ViewCtx, results map[string]any) DetailView {
	state := Txt("detail.sab.loading")
	if data.Paused {
		state = Txt("detail.sab.paused")
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.state"), Value: state}, {Label: T("detail.sab.slots"), Value: data.Slots}},
		Facts: []Kpi{{Value: NumU(data.SpeedKB/kbPerMB, 1, "MB/s"), Label: T("detail.sab.speed")}, {Value: NumU(data.FreeGB, 0, "GB"), Label: T("detail.sab.free")},
			{Value: len(data.Failures), Label: T("detail.sab.failed"), Tier: tierIf(len(data.Failures) > 0, "yellow", "")}}}
	var queue []ShareBar
	for _, q := range data.Queue {
		queue = append(queue, ShareBar{Name: q.Name, Pct: q.Percent, Value: q.Left})
	}
	if len(queue) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("detail.sab.queue"), Data: queue})
	}
	var failed []Event
	for _, f := range data.Failures {
		failed = append(failed, Event{At: f.At, Title: f.Name, Sub: f.Reason, State: Txt("detail.sab.failed_one"), Tier: "red"})
	}
	if len(failed) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.sab.failures"), Data: failed})
	}
	// Loaded per day over four weeks (server_stats, fetched on open).
	if st, ok := results[openName].(*sources.SabStats); ok {
		now := time.Now()
		days := make([]float64, sabDays)
		for i := range days {
			days[i] = st.Daily[now.AddDate(0, 0, i-sabDays+1).Format(time.DateOnly)] / bytesPerGB
		}
		g := ColGraph(days, "s1")
		g.Ticks = spanTicks(now, sabDays)
		body.Line = append(body.Line, Fact{Label: T("detail.sab.month"), Value: NumU(st.Month/bytesPerGB, 0, "GB")},
			Fact{Label: T("detail.sab.total"), Value: NumU(st.Total/bytesPerTB, 1, "TB")})
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.sab.volume"), Meta: NumU(st.Week/bytesPerGB, 0, "GB"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.sab.loading", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if data.Paused {
		head.State, head.StateKey = "off", "detail.sab.paused"
	}
	return DetailView{Head: head, Body: body}
}

const (
	kbPerMB = 1024
	sabDays = 28
)

// rssDetail (large view): the picked entry (default newest) to read, the
// entries aside.
func rssDetail(cfg RssConfig, results map[string]any, _ ViewCtx) DetailView {
	feed, ok := results["feed"].(*sources.FeedResult)
	if !ok || len(feed.Items) == 0 {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.rss.empty")}}}}
	}
	first := feed.Items[0]
	for _, it := range feed.Items {
		if it.Link != "" && it.Link == pickedItem(results) {
			first = it
		}
	}
	read := Reading{Title: first.Title, Link: first.Link}
	for _, p := range strings.Split(strings.TrimSpace(first.Summary), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			read.Text = append(read.Text, p)
		}
	}

	// The page's full text, read on open, replaces the feed's summary.
	if full, ok := results[openName].(*sources.Article); ok && full.Link == first.Link && len(full.Paragraphs) > 0 {
		read.Text = full.Paragraphs
	}
	body := &DetailBody{End: []Fact{{Label: T("detail.rss.feed"), Value: feed.Title}, {Label: T("detail.rss.published"), Value: dayOrDash(isoDayOf(first.Published))}}}
	if first.Image != "" {
		body.Blocks = append(body.Blocks, Block{Kind: BlockImage, Data: Image{DataURI: first.Image, Alt: first.Title}})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockRead, Data: read})
	var more []LitRow
	for _, it := range feed.Items {
		if it.Link == first.Link {
			continue
		}
		more = append(more, LitRow{Name: it.Title, Meta: dayOrDash(isoDayOf(it.Published)), State: "info", Item: it.Link})
	}
	if len(more) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.rss.more"), Data: firstN(more, mediaListLimit)})
	}
	return DetailView{Head: DetailHead{Sub: feed.Title}, Body: body}
}

// pictureDetail (large view): APOD or xkcd at full size with its text.
func pictureDetail(cfg PictureConfig, results map[string]any, _ ViewCtx) DetailView {
	p, ok := results["picture"].(*sources.Picture)
	if !ok || p.DataURI == "" {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.picture.none")}}}}
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockImage, Data: Image{DataURI: p.DataURI, Alt: p.Title}}}}
	body.End = []Fact{{Label: T("detail.picture.title"), Value: p.Title}}
	if p.Text != "" {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRead, Data: Reading{Text: []string{p.Text}, Link: p.Link}})
	}
	// APOD: the days before, as a strip (fetched on open).
	if past, ok := results[openName].(*sources.PictureList); ok && len(past.Items) > 0 {
		var thumbs []Image
		for _, x := range past.Items {
			thumbs = append(thumbs, Image{DataURI: x.DataURI, Alt: x.Title, Caption: x.Day + " · " + x.Title})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockThumbs, Label: T("detail.picture.archive"), Data: thumbs})
	}
	head := DetailHead{}
	if p.Link != "" {
		head.Actions = []DetailAction{{LabelKey: "detail.picture.original", Href: p.Link, Primary: true}}
	}
	if p.Explain != "" {
		head.Actions = append(head.Actions, DetailAction{LabelKey: "detail.picture.explain", Href: p.Explain})
	}
	return DetailView{Head: head, Body: body}
}

// imageDetail (large view): the picture at full size.
func imageDetail(cfg ImageConfig, results map[string]any, _ ViewCtx) DetailView {
	img, ok := results["image"].(*sources.ImageResult)
	if !ok || img.DataURI == "" {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.picture.none")}}}}
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockImage, Data: Image{DataURI: img.DataURI, Alt: cfg.URL}}},
		End: []Fact{{Label: T("detail.picture.source"), Value: cfg.URL}}}
	head := DetailHead{}
	if cfg.Link != "" {
		head.Actions = []DetailAction{{LabelKey: "detail.picture.original", Href: cfg.Link, Primary: true}}
	}
	return DetailView{Head: head, Body: body}
}

// isoDayOf is the day part of an ISO 8601 time: "2026-09-16T08:00:00Z" → "2026-09-16".
func isoDayOf(s string) string { return s[:min(len(s), len(isoDate))] }
