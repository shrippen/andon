package widgets

// "lemmy": the account's unread replies and mentions, then the hot posts
// of its subscribed communities; the dialog adds the account's own posts
// with their votes.
//
//	1 unread
//	● cutter_jo   replied in c/kde · 5 h
//	This timecode clock widget …   c/kde · 96 points · 18 comments

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

// LemmyConfig is the "lemmy" widget's config.
type LemmyConfig struct{ Limit int }

const lemmyShown = 5

func init() {
	Tile[LemmyConfig]{Key: "lemmy", Detail: dataDetail(lemmyDetail), Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceLemmy, RefreshS: 600,
		Fields:  []Field{{Key: "limit", Input: InputNumber, Default: lemmyShown, Min: "1", Max: "20"}},
		Decode:  func(r Raw) LemmyConfig { return LemmyConfig{Limit: r.Int("limit")} },
		Queries: ownData[LemmyConfig], View: dataView(lemmyView),
		Calm: func(v map[string]any) bool { return v["Unread"] == 0 }}.add()
}

func lemmyView(cfg LemmyConfig, data *sources.LemmyDataset, _ ViewCtx) map[string]any {
	notes := firstN(data.Replies, cfg.Limit)
	return map[string]any{"Unread": len(data.Replies), "Notes": notes, "Posts": firstN(data.Subscribed, max(cfg.Limit-len(notes), 1))}
}

func lemmyDetail(_ LemmyConfig, data *sources.LemmyDataset, _ ViewCtx, results map[string]any) DetailView {
	body := &DetailBody{Facts: []Kpi{{Value: len(data.Replies), Label: T("lemmy.unread"), Tier: tierIf(len(data.Replies) > 0, "yellow", "green")},
		{Value: data.User.Posts, Label: T("lemmy.posts")}, {Value: data.User.Comments, Label: T("lemmy.comments")}, {Value: orNone(data.Version), Label: T("lemmy.version")}}}
	if len(data.Replies) > 0 {
		var rows [][]Cell
		for _, n := range data.Replies {
			rows = append(rows, []Cell{{Value: Txt("lemmy.kind." + n.Kind)}, {Value: n.From}, {Value: orNone(n.Text), Href: n.URL}, {Value: "c/" + n.Community}, {Value: agoOf(n.At)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("lemmy.inbox"), Data: Table{
			Head: []Text{T("lemmy.col.kind"), T("lemmy.col.from"), T("lemmy.col.text"), T("lemmy.col.community"), T("lemmy.col.when")}, Rows: rows}})
	}
	for _, list := range []struct {
		label string
		posts []sources.LemmyPost
	}{{"lemmy.hot", data.Subscribed}, {"lemmy.own", data.Own}} {
		if len(list.posts) == 0 {
			continue
		}
		var rows [][]Cell
		for _, p := range list.posts {
			rows = append(rows, []Cell{{Value: p.Title, Href: p.URL}, {Value: "c/" + p.Community}, {Value: p.Score}, {Value: p.Comments, Href: p.Link}, {Value: agoOf(p.At)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T(list.label), Data: Table{
			Head: []Text{T("lemmy.col.title"), T("lemmy.col.community"), T("lemmy.col.score"), T("lemmy.col.comments"), T("lemmy.col.when")}, Rows: rows, Num: []int{2, 3}}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
