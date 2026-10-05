package widgets

// "github_downloads": release downloads of the connection's repos. GitHub
// only keeps a total per asset; the analysis records it daily (see
// metrics.DownloadsKey), so the trend is the difference between days:
//
//	totals  4012 4050 4092 …   (history, per repo)
//	gains        38   42 …     (metrics.DailyGains) ─► bars, repo rows

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

// DownloadsConfig is the "github_downloads" widget's config.
type DownloadsConfig struct {
	Only  []string // repo name parts (lower case), empty = all
	Days  int
	Limit int // repo rows on the tile
}

// RepoDownloads is one repo's downloads: total, and per day in the window.
type RepoDownloads struct {
	Name    string
	Total   float64
	Gain    float64   // added within the window
	Pct     int       // Gain against the best repo's, for the bar
	Days    []float64 // per day, oldest first; Gap = not recorded
	Release string
	At      time.Time
}

func init() {
	Tile[DownloadsConfig]{Key: "github_downloads", Detail: downloadsDetail, Category: CategoryInsight, Topic: TopicDev, Service: enums.ServiceGitHub,
		RefreshS: integrationTTL, Extra: ExtraHistory,
		Fields: []Field{{Key: "filter", Input: InputList}, {Key: "days", Input: InputNumber, Default: downloadDays, Min: "2", Max: "90"}, pickLimit},
		Decode: func(r Raw) DownloadsConfig {
			return DownloadsConfig{Only: r.Lower("filter"), Days: r.Int("days"), Limit: r.Int("limit")}
		},
		Queries: ownData[DownloadsConfig], View: downloadsView}.add()
}

// repoDownloads lists the shown repos with downloads, most gained first.
func repoDownloads(cfg DownloadsConfig, data *sources.GitHubDataset, h *metrics.History, now time.Time) []RepoDownloads {
	var out []RepoDownloads
	for _, r := range data.Repos {
		if r.Downloads == 0 || !matchesAny(r.Name, cfg.Only) {
			continue
		}

		row := RepoDownloads{Name: r.Name, Total: float64(r.Downloads), Release: r.Release, At: r.ReleasedAt,
			Days: onDays(metrics.DailyGains(h.SeriesOf(metrics.DownloadsKey(r.Name))), now, cfg.Days)}
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

// sumDays adds the repos up per day; a day no repo recorded is a Gap.
func sumDays(repos []RepoDownloads, n int) []float64 {
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

func downloadsView(cfg DownloadsConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results[dataName].(*sources.GitHubDataset)
	if !ok {
		return map[string]any{}
	}

	repos := repoDownloads(cfg, data, historyOf(results), todayOf(ctx))
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

// downloadsDetail (grid): the window per day, then every repo with its
// total, its gain and its latest release.
func downloadsDetail(cfg DownloadsConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results[dataName].(*sources.GitHubDataset)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}

	now := todayOf(ctx)
	repos := repoDownloads(cfg, data, historyOf(results), now)
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
		release := any("–")
		if r.Release != "" {
			release = TxtA("detail.git.release", "name", r.Release, "day", Day(r.At))
		}
		rows = append(rows, []Cell{{Value: r.Name, Href: githubWeb + r.Name + "/releases"}, {Value: Num(r.Total, 0)}, {Value: Num(r.Gain, 0)}, {Value: release}})
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.git.repos"), Data: Table{
			Head: []Text{T("detail.git.repo"), T("detail.downloads.total"), T("detail.downloads.window"), T("detail.git.latest")}, Rows: rows, Num: []int{1, 2}}})
	}
	body.Side = downloadsRead(data)
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

// githubWeb precedes "owner/name" on GitHub's site.
const githubWeb = "https://github.com/"
