package widgetlib

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/history"
	"andon/internal/services/svcdata"
	"andon/internal/services/weekly"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// Detail dialogs of tiles. The frame (head, close, loading, reload) is the
// same for every type; the body comes from the type's Detail function (or
// a loader of its own) and its template "details/<type>", which picks one
// of Kante's layouts:
//
//	GET /details/{id}[?item=x] ─► boards.Detail (right check) ─► LoadDetail
//	    ├─ detailKinds[type]   own loader (link: its check history)
//	    └─ loadTileDetail      the tile's pipeline + history + hints ─► kind.Detail
//	─► DetailDialog{Head, Body} ─► template "details/<type>"
//
// A new popup: Detail on the type's Tile and a template.

// ErrNoDetail: the tile has no detail dialog (type without one, or a link
// without status check).
var ErrNoDetail = errors.New("tile without detail dialog")

// DetailDialog is one dialog: the head every type shares and the type's data.
type DetailDialog struct {
	Type string
	Head DetailHead
	Body any
}

// DetailHead and DetailAction are the head as widgets declare it.
type (
	DetailHead   = widgets.DetailHead
	DetailAction = widgets.DetailAction
)

// detailExtra loads what one type's dialog needs from Andon itself for a
// picked entry (a hint's history), into results.
type detailExtra func(ctx context.Context, d *sql.DB, who *access.Principal, item string, results map[string]any) error

// detailExtras are the types with such a loader.
var detailExtras = map[string]detailExtra{
	"hints":      hintWork,
	"rss":        feedArticle,
	"week_story": storyWeek,
}

// feedArticle reads the page of the picked entry; only links of the feed
// qualify, so the dialog never fetches an arbitrary address.
func feedArticle(ctx context.Context, d *sql.DB, _ *access.Principal, item string, results map[string]any) error {
	feed, ok := results["feed"].(*sources.FeedResult)
	if !ok || !slices.ContainsFunc(feed.Items, func(it sources.FeedItem) bool { return it.Link == item }) {
		return nil
	}
	res, err := svcdata.Get(ctx, d, sources.ArticleSource.Key(), map[string]any{"link": item}, nil, nil, svcdata.Cached)
	if err != nil || !res.Ok() {
		return nil
	}
	results[widgets.ArticleSlot] = res.Data
	return nil
}

// storyArchive is how many weeks back the story dialog reaches.
const storyArchive = 8

// storyWeek replaces the story by the one of a past week (item: weeks
// back, 1 = last week); the story is drawn from what is stored.
func storyWeek(ctx context.Context, d *sql.DB, who *access.Principal, item string, results map[string]any) error {
	back, err := strconv.Atoi(item)
	if err != nil || back < 1 || back > storyArchive {
		return nil
	}
	start := metrics.WeekStart(metrics.Today(time.Now().UTC())).AddDate(0, 0, -7*back)
	lines, err := weekly.StorySince(ctx, d, who, start, start.AddDate(0, 0, 7))
	if err != nil {
		return err
	}
	results[widgets.StorySlot] = lines
	return nil
}

// hintWork loads the picked hint's work, history and possible assignees;
// a hint the viewer cannot reach is simply none.
func hintWork(_ context.Context, d *sql.DB, who *access.Principal, item string, results map[string]any) error {
	id, err := strconv.ParseInt(item, 10, 64)
	if err != nil {
		return nil
	}
	h, err := hints.One(d, who, id)
	if err != nil {
		return nil
	}
	work := widgets.HintWork{ID: h.ID, Title: h.Title, Why: h.Why, Assignee: h.Assignee, Work: string(h.Work)}
	if h.AssigneeID != nil {
		work.AssigneeID = *h.AssigneeID
	}
	steps, err := hints.History(d, who, id)
	if err != nil {
		return err
	}
	for _, s := range steps {
		work.History = append(work.History, widgets.HintStep{At: s.At, Kind: string(s.Kind), Note: s.Note, Actor: s.Actor})
	}
	people, err := hints.Assignees(d, who, id)
	if err != nil {
		return err
	}
	for _, p := range people {
		work.People = append(work.People, widgets.FormOption{Value: strconv.FormatInt(p.ID, 10), Label: p.Name})
	}
	results[widgets.HintWorkSlot] = work
	return nil
}

// detailHints caps the hints a dialog lists.
const detailHints = 5

// detailLoader reads a type's dialog; the caller checked the right on the
// widget.
type detailLoader func(ctx context.Context, d *sql.DB, who *access.Principal, widget *model.Widget, now time.Time) (*DetailDialog, error)

// detailKinds are types with a loader of their own; every other type with
// a Detail function goes through loadTileDetail.
var detailKinds = map[string]detailLoader{
	"link": loadLinkDetail,
}

// HasDetail tells whether tiles of a type open a detail dialog.
func HasDetail(widgetType string) bool {
	if _, ok := detailKinds[widgetType]; ok {
		return true
	}
	kind, ok := widgets.Get(widgetType)
	return ok && kind.Detail != nil
}

// LoadDetail loads a placed tile's dialog; the caller checked the
// viewer's right on the widget.
// item is the list entry the viewer picked ("" = none).
func LoadDetail(ctx context.Context, d *sql.DB, who *access.Principal, widget *model.Widget, item string, now time.Time) (*DetailDialog, error) {
	if load, ok := detailKinds[widget.Type]; ok {
		return load(ctx, d, who, widget, now)
	}
	if !HasDetail(widget.Type) {
		return nil, ErrNoDetail
	}
	return loadTileDetail(ctx, d, who, widget, item, now, originStored)
}

// loadTileDetail runs the tile's own pipeline (stored data, so opening a
// dialog fetches nothing), adds the space's history, the open hints of the
// tile's services and the tile's view, and hands all to kind.Detail.
// from = originDemo draws it from demo datasets (tests).
func loadTileDetail(ctx context.Context, d *sql.DB, who *access.Principal, widget *model.Widget, item string, now time.Time, from origin) (*DetailDialog, error) {
	kind, _ := widgets.Get(widget.Type)
	fresh := svcdata.Stored
	if from == originDemo {
		fresh = svcdata.Cached
	}
	frag, err := load(ctx, d, who, widget, fresh, from, loadDetail)
	if err != nil {
		return nil, err
	}

	results := make(map[string]any, len(frag.results)+4)
	for k, v := range frag.results {
		results[k] = v
	}
	results[widgets.DetailItemSlot] = item
	if _, ok := results[widgets.HistorySlot]; !ok {
		h, err := history.Load(d, widget.SpaceID, 0, now)
		if err != nil {
			return nil, err
		}
		results[widgets.HistorySlot] = h
	}
	if len(frag.services) > 0 {
		found, err := hints.Filtered(d, who, hints.Filter{Sources: frag.services}, 0)
		if err != nil {
			return nil, err
		}
		results[widgets.DetailHintsSlot] = detailHintsOf(ofConnections(found, frag.hintConns))
	}
	if frag.View != nil {
		results[widgets.TileViewSlot] = frag.View
		// A hint tile's own list (topic, filter) beats the service filter.
		if own, ok := frag.View["Hints"].([]hints.View); ok {
			results[widgets.DetailHintsSlot] = detailHintsOf(own)
		}
	}

	if extra, ok := detailExtras[widget.Type]; ok && item != "" && from == originStored {
		if err := extra(ctx, d, who, item, results); err != nil {
			return nil, err
		}
	}
	view := kind.Detail(frag.Config, results, frag.viewCtx)
	view.Head.Title = widget.Title
	return &DetailDialog{Type: widget.Type, Head: view.Head, Body: view.Body}, nil
}

// detailHintsOf turns hints into the form a dialog lists.
func detailHintsOf(found []hints.View) []widgets.DetailHint {
	list := make([]widgets.DetailHint, len(found))
	for i, h := range found {
		list[i] = widgets.DetailHint{ID: h.ID, Rule: h.Rule, Severity: h.Severity, Title: h.Title, Why: h.Why, FirstSeen: h.FirstSeen, Due: h.Due}
	}
	return list
}

// ofConnections keeps the hints of the given connections (and those tied
// to none), at most detailHints: a viewer with two Paperless connections
// sees only the tile's.
func ofConnections(found []hints.View, conns []int64) []hints.View {
	var out []hints.View
	for _, h := range found {
		if h.ConnectionID != nil && !slices.Contains(conns, *h.ConnectionID) {
			continue
		}
		out = append(out, h)
		if len(out) == detailHints {
			break
		}
	}
	return out
}
