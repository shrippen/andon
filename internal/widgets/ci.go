package widgets

// "ci_runs": the CI of every repository the space's Drone, GitHub and
// Gitea connections know (sources.CISource), red ones first. The dialog
// draws each repo's last runs as a strip and names failing steps.
//
//	studio/showreel  Drone   ✕ ✕ ✓ ✓   red since yesterday
//	studio/website   GitHub  ✓          (GitHub tells the latest run only)

import (
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// CIConfig is the "ci_runs" widget's config.
type CIConfig struct {
	OnlyRed bool
	Limit   int
}

const ciShown = 6

// ciServices are the providers the tile reads.
var ciServices = []enums.ServiceType{enums.ServiceDrone, enums.ServiceGitHub, enums.ServiceGitea}

func init() {
	Tile[CIConfig]{Key: "ci_runs", Detail: ciDetail, Category: CategoryInsight, Topic: TopicDev, RefreshS: 600,
		Fields: []Field{{Key: "only_problems", Input: InputCheck}, {Key: "limit", Input: InputNumber, Default: ciShown, Min: "1", Max: "30"}},
		Decode: func(r Raw) CIConfig { return CIConfig{OnlyRed: r.Bool("only_problems"), Limit: r.Int("limit")} },
		Queries: func(CIConfig) []Query {
			var out []Query
			for _, s := range ciServices {
				out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
			}
			return out
		},
		View: ciView,
		Calm: func(v map[string]any) bool { return v["Total"] != nil && v["Total"] != 0 && v["Red"] == 0 }}.add()
}

// ciRank sorts red first, then running, then the rest.
var ciRank = map[sources.CIStatus]int{sources.CIFailed: 0, sources.CIRunning: 1}

// ciRepos collects the repos of every CI source, red first, then by name.
func ciRepos(results map[string]any) []sources.CIRepo {
	var out []sources.CIRepo
	for _, s := range ciServices {
		if src, ok := results[string(s)].(sources.CISource); ok {
			out = append(out, src.CIRepos()...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, okI := ciRank[out[i].Status]
		rj, okJ := ciRank[out[j].Status]
		if okI != okJ {
			return okI
		}
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Repo) < strings.ToLower(out[j].Repo)
	})
	return out
}

// ciPill is the Kante pill state of a CI status.
var ciPill = map[sources.CIStatus]string{sources.CIOK: "applied", sources.CIFailed: "failed", sources.CIRunning: "analyzing"}

// CILine is one repo on the tile.
type CILine struct {
	sources.CIRepo
	Pill string
}

func ciView(cfg CIConfig, results map[string]any, _ ViewCtx) map[string]any {
	repos := ciRepos(results)
	red := 0
	var lines []CILine
	for _, r := range repos {
		if r.Status == sources.CIFailed {
			red++
		}
		if (cfg.OnlyRed && r.Status != sources.CIFailed) || len(lines) >= cfg.Limit {
			continue
		}
		lines = append(lines, CILine{CIRepo: r, Pill: ciPill[r.Status]})
	}
	return map[string]any{"Total": len(repos), "Red": red, "Lines": lines}
}

// ciStrip is the Kante strip state of a run.
var ciStrip = map[sources.CIStatus]string{sources.CIOK: "ok", sources.CIFailed: "bad", sources.CIRunning: "warn"}

func ciDetail(_ CIConfig, results map[string]any, _ ViewCtx) DetailView {
	repos := ciRepos(results)
	counts := map[sources.CIStatus]int{}
	var strips []Strip
	var failing [][]Cell
	for _, r := range repos {
		counts[r.Status]++
		runs := r.Runs
		if len(runs) == 0 {
			runs = []sources.CIRun{{Status: r.Status}}
		}
		strip := Strip{Name: r.Repo, Value: Txt("service." + string(r.Provider))}
		for i := len(runs) - 1; i >= 0; i-- {
			state, ok := ciStrip[runs[i].Status]
			if !ok {
				state = "off"
			}
			strip.States = append(strip.States, state)
		}
		strips = append(strips, strip)
		if r.Status == sources.CIFailed {
			failing = append(failing, []Cell{{Value: r.Repo, Href: r.URL}, {Value: Txt("service." + string(r.Provider))}, {Value: orNone(r.Step)},
				{Value: ciLastRun(r)}})
		}
	}
	body := &DetailBody{Facts: []Kpi{{Value: counts[sources.CIFailed], Label: T("ci.red"), Tier: tierIf(counts[sources.CIFailed] > 0, "red", "green")},
		{Value: counts[sources.CIOK], Label: T("ci.green"), Tier: "green"}, {Value: counts[sources.CIRunning], Label: T("ci.running")}}}
	if len(failing) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("ci.failing"), Data: Table{
			Head: []Text{T("ci.col.repo"), T("ci.col.provider"), T("ci.col.step"), T("ci.col.last")}, Rows: failing}})
	}
	if len(strips) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockStrips, Label: T("ci.runs"), Meta: len(strips), Data: strips, Ticks: []any{Txt("ci.older"), Txt("ci.newest")}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// ciLastRun is when the newest run started, "–" when unknown.
func ciLastRun(r sources.CIRepo) any {
	if len(r.Runs) == 0 {
		return "–"
	}
	return agoOf(r.Runs[0].Started)
}
