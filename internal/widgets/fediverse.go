package widgets

// "fediverse": the account's followers with what the last days added,
// and its unread notifications; the dialog draws the followers over the
// recorded days and lists notifications and own posts.
//
//	412 followers  +6 in 7 days
//	● elbefilmclub@film…  mentioned you   3 h
//	● nordlicht@social…   follows you     9 h

import (
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// FediConfig is the "fediverse" widget's config.
type FediConfig struct {
	Days  int
	Limit int
}

const (
	fediDays  = 7
	fediShown = 4
	// fediChartDays is how far the dialog's chart looks back.
	fediChartDays = 30
)

func init() {
	Tile[FediConfig]{Key: "fediverse", Detail: dataDetail(fediDetail), Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceFediverse,
		RefreshS: 600, Extra: ExtraHistory,
		Fields: []Field{{Key: "days", Input: InputNumber, Default: fediDays, Min: "1", Max: "30"},
			{Key: "limit", Input: InputNumber, Default: fediShown, Min: "1", Max: "20"}},
		Decode:  func(r Raw) FediConfig { return FediConfig{Days: r.Int("days"), Limit: r.Int("limit")} },
		Queries: ownData[FediConfig], View: fediView,
		Calm: func(v map[string]any) bool { return v["Unread"] == 0 }}.add()
}

// followerDays are the recorded followers of the last n days, oldest
// first, Gap where none was recorded.
func followerDays(data *sources.FediverseDataset, results map[string]any, ctx ViewCtx, n int) []float64 {
	return onDays(historyOf(results).SeriesOf(metrics.FollowersKey(data.Account.Acct)), todayOf(ctx), n)
}

// gainOf is the last value less the first one recorded, 0 with fewer
// than two.
func gainOf(values []float64) int {
	first, last, seen := 0.0, 0.0, 0
	for _, v := range values {
		if v != v {
			continue
		}
		if seen == 0 {
			first = v
		}
		last, seen = v, seen+1
	}
	if seen < 2 {
		return 0
	}
	return int(last - first)
}

func fediView(cfg FediConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results[dataName].(*sources.FediverseDataset)
	if !ok {
		return map[string]any{}
	}
	unread := data.Unread("")
	return map[string]any{"Account": data.Account, "Gain": gainOf(followerDays(data, results, ctx, cfg.Days+1)), "Days": cfg.Days,
		"Unread": len(unread), "Notes": firstN(unread, cfg.Limit)}
}

func fediDetail(_ FediConfig, data *sources.FediverseDataset, ctx ViewCtx, results map[string]any) DetailView {
	a := data.Account
	version := orNone(data.Version)
	if data.Software != "" {
		version = data.Software + " " + data.Version
	}
	body := &DetailBody{Facts: []Kpi{{Value: a.Followers, Label: T("fediverse.followers")}, {Value: a.Following, Label: T("fediverse.following")},
		{Value: a.Posts, Label: T("fediverse.posts")}, {Value: len(data.Unread("")), Label: T("fediverse.unread")}, {Value: version, Label: T("fediverse.version")}}}

	days := followerDays(data, results, ctx, fediChartDays)
	if countValues(days) > 1 {
		today := todayOf(ctx)
		labels := make([]any, len(days))
		for i := range days {
			labels[i] = DayS(today.AddDate(0, 0, i-len(days)+1).Format(time.DateOnly))
		}
		g := LineGraph(Series{Values: days, Class: "s1", Label: Txt("fediverse.followers")})
		g.Labels, g.Ticks = labels, []any{labels[0], labels[len(labels)-1]}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("fediverse.followers_days"), Hero: true, Data: g})
	}
	if len(data.Notes) > 0 {
		var rows [][]Cell
		for _, n := range data.Notes {
			rows = append(rows, []Cell{{Value: Txt("fediverse.type." + n.Type)}, {Value: n.From}, {Value: orNone(n.Text), Href: n.URL},
				{Value: agoOf(n.At), State: stateIf(n.Unread, "warn")}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("fediverse.notes"), Data: Table{
			Head: []Text{T("fediverse.col.type"), T("fediverse.col.from"), T("fediverse.col.text"), T("fediverse.col.when")}, Rows: rows}})
	}
	if len(data.Posts) > 0 {
		var rows [][]Cell
		for _, p := range data.Posts {
			rows = append(rows, []Cell{{Value: p.Text, Href: p.URL}, {Value: agoOf(p.At)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("fediverse.own_posts"), Data: Table{
			Head: []Text{T("fediverse.col.text"), T("fediverse.col.when")}, Rows: rows}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// countValues counts the values that are not a Gap.
func countValues(values []float64) int {
	n := 0
	for _, v := range values {
		if v == v {
			n++
		}
	}
	return n
}
