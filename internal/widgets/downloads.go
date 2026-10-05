package widgets

// "github_downloads", "kdestore_downloads": downloads of a connection's
// published items (sources.Downloadable). The services only keep a
// running total; the analysis records it daily (see metrics.DownloadsKey),
// so the trend is the difference between days:
//
//	totals  4012 4050 4092 …   (history, per item)
//	gains        38   42 …     (metrics.DailyGains) ─► bars, item rows

import (
	"sort"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// downloadDays is the default window of the trend.
const downloadDays = 30

// DownloadsConfig is the downloads widgets' config.
type DownloadsConfig struct {
	Only  []string // item name parts (lower case), empty = all
	Days  int
	Limit int // item rows on the tile
}

// ItemDownloads is one item's downloads: total, and per day in the window.
type ItemDownloads struct {
	Name    string
	URL     string
	Total   float64
	Gain    float64   // added within the window
	Pct     int       // Gain against the best item's, for the bar
	Days    []float64 // per day, oldest first; Gap = not recorded
	Version string
	At      time.Time
}

// downloadsKind is what differs between the services: their own words
// for an item and its version in the detail table.
type downloadsKind struct {
	service              enums.ServiceType
	item, items, version string // catalog keys
}

var downloadKinds = map[string]downloadsKind{
	"github_downloads":   {service: enums.ServiceGitHub, item: "detail.git.repo", items: "detail.git.repos", version: "detail.git.latest"},
	"kdestore_downloads": {service: enums.ServiceKDEStore, item: "detail.downloads.entry", items: "detail.downloads.entries", version: "detail.downloads.version"},
}

func init() {
	for key, kind := range downloadKinds {
		Tile[DownloadsConfig]{Key: key, Detail: downloadsDetail(kind), Category: CategoryInsight, Topic: TopicDev, Service: kind.service,
			RefreshS: integrationTTL, Extra: ExtraHistory,
			Fields: []Field{{Key: "filter", Input: InputList}, {Key: "days", Input: InputNumber, Default: downloadDays, Min: "2", Max: "90"}, pickLimit},
			Decode: func(r Raw) DownloadsConfig {
				return DownloadsConfig{Only: r.Lower("filter"), Days: r.Int("days"), Limit: r.Int("limit")}
			},
			Queries: ownData[DownloadsConfig], View: downloadsView(kind)}.add()
	}
}

// itemDownloads lists the shown items with downloads, most gained first.
func itemDownloads(cfg DownloadsConfig, service enums.ServiceType, items []sources.DownloadItem, h *metrics.History, now time.Time) []ItemDownloads {
	var out []ItemDownloads
	for _, it := range items {
		if it.Total == 0 || !matchesAny(it.Name, cfg.Only) {
			continue
		}

		row := ItemDownloads{Name: it.Name, URL: it.URL, Total: float64(it.Total), Version: it.Version, At: it.Released,
			Days: onDays(metrics.DailyGains(h.SeriesOf(metrics.DownloadsKey(service, it.ID))), now, cfg.Days)}
		for _, v := range row.Days {
			if v == v {
				row.Gain += v
			}
		}
		out = append(out, row)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Gain > out[j].Gain })
	for i := range out {
		out[i].Pct = pctOf(out[i].Gain, out[0].Gain)
	}
	return out
}

// sumDays adds the items up per day; a day no item recorded is a Gap.
func sumDays(repos []ItemDownloads, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = Gap
	}
	for _, r := range repos {
		for i, v := range r.Days {
			if v != v {
				continue
			}
			if out[i] != out[i] {
				out[i] = 0
			}
			out[i] += v
		}
	}
	return out
}

func downloadsView(kind downloadsKind) func(DownloadsConfig, map[string]any, ViewCtx) map[string]any {
	return func(cfg DownloadsConfig, results map[string]any, ctx ViewCtx) map[string]any {
		data, ok := results[dataName].(sources.Downloadable)
		if !ok {
			return map[string]any{}
		}
		return downloadsTile(cfg, itemDownloads(cfg, kind.service, data.Downloads(), historyOf(results), todayOf(ctx)), ctx)
	}
}

// downloadsTile is the tile's view of the shown items.
func downloadsTile(cfg DownloadsConfig, repos []ItemDownloads, ctx ViewCtx) map[string]any {
	daily := sumDays(repos, cfg.Days)
	total, gain, top := 0.0, 0.0, 0.0
	for _, r := range repos {
		total, gain = total+r.Total, gain+r.Gain
	}
	for _, v := range daily {
		if v == v {
			top = max(top, v)
		}
	}

	// A day without a record is a flat bar, not a missing one: the row
	// keeps its dates in place.
	var bars []LoadBar
	first := metrics.Today(todayOf(ctx)).AddDate(0, 0, -(cfg.Days - 1))
	for i, v := range daily {
		if v != v {
			v = 0
		}
		bars = append(bars, LoadBar{H: max(pctOf(v, top), 2), Title: first.AddDate(0, 0, i).Format(time.DateOnly)})
	}
	out := map[string]any{"Total": total, "Gain": gain, "Days": cfg.Days, "Repos": firstN(repos, cfg.Limit), "Count": len(repos)}
	if hasValues(daily) {
		out["Bars"] = bars
	}
	return out
}

// downloadsDetail (grid): the window per day, then every item with its
// total, its gain and its latest version.
func downloadsDetail(kind downloadsKind) func(DownloadsConfig, map[string]any, ViewCtx) DetailView {
	return func(cfg DownloadsConfig, results map[string]any, ctx ViewCtx) DetailView {
		data, ok := results[dataName].(sources.Downloadable)
		if !ok {
			return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
		}
		return downloadsSheet(cfg, kind, data, results, todayOf(ctx))
	}
}

// downloadsSheet is the detail of the shown items.
func downloadsSheet(cfg DownloadsConfig, kind downloadsKind, data sources.Downloadable, results map[string]any, now time.Time) DetailView {
	repos := itemDownloads(cfg, kind.service, data.Downloads(), historyOf(results), now)
	daily := sumDays(repos, cfg.Days)
	total, gain := 0.0, 0.0
	for _, r := range repos {
		total, gain = total+r.Total, gain+r.Gain
	}
	best, bestDay, days := 0.0, -1, 0
	for i, v := range daily {
		if v != v {
			continue
		}
		days++
		if bestDay < 0 || v > best {
			best, bestDay = v, i
		}
	}

	body := &DetailBody{Facts: []Kpi{{Value: Num(total, 0), Label: T("detail.downloads.total")}, {Value: Num(gain, 0), Label: Text{Key: "detail.downloads.gained", Args: map[string]any{"days": cfg.Days}}}}}
	if days > 0 {
		body.Facts = append(body.Facts, Kpi{Value: Num(gain/float64(days), 0), Label: T("detail.downloads.per_day")})
		g := ColGraph(daily, "s1")
		g.Ticks = spanTicks(now, cfg.Days)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.downloads.per_day"), Hero: true, Data: g,
			Meta: TxtA("detail.downloads.best", "n", int(best), "day", Day(metrics.Today(now).AddDate(0, 0, bestDay-(cfg.Days-1))))})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("trend.collecting")})
	}

	var rows [][]Cell
	for _, r := range repos {
		version := any("–")
		if r.Version != "" {
			version = TxtA("detail.git.release", "name", r.Version, "day", Day(r.At))
		}
		rows = append(rows, []Cell{{Value: r.Name, Href: r.URL}, {Value: Num(r.Total, 0)}, {Value: Num(r.Gain, 0)}, {Value: version}})
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T(kind.items), Data: Table{
			Head: []Text{T(kind.item), T("detail.downloads.total"), T("detail.downloads.window"), T(kind.version)}, Rows: rows, Num: []int{1, 2}}})
	}
	if gh, ok := data.(*sources.GitHubDataset); ok {
		body.Side = downloadsRead(gh)
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// downloadsRead says when the oldest total was read and how often they
// are: "Gezählt: vor 12 Minuten · Abfrage: alle 1 h, automatisch".
func downloadsRead(data *sources.GitHubDataset) []Fact {
	if data.DownloadsEvery == 0 {
		return nil
	}

	var oldest time.Time
	for _, r := range data.Repos {
		if !r.DownloadsAt.IsZero() && (oldest.IsZero() || r.DownloadsAt.Before(oldest)) {
			oldest = r.DownloadsAt
		}
	}
	every := strconv.Itoa(int(data.DownloadsEvery.Minutes())) + " min"
	if data.DownloadsEvery%time.Hour == 0 {
		every = strconv.Itoa(int(data.DownloadsEvery.Hours())) + " h"
	}
	key := "detail.downloads.every_fixed"
	if data.DownloadsAuto {
		key = "detail.downloads.every_auto"
	}
	return []Fact{{Label: T("detail.downloads.read"), Value: agoOf(oldest)}, {Label: T("detail.downloads.every"), Value: TxtA(key, "time", every)}}
}
