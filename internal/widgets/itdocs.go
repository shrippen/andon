package widgets

// ── docs_coverage ──
//
// How much of the compose stacks the IT docs cover, per host, and the
// stacks still without a note (Phase 15, rules docs.*).

import (
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// DocsConfig is the "docs_coverage" widget's config.
type DocsConfig struct{ List bool }

func init() {
	Tile[DocsConfig]{Key: "docs_coverage", Detail: dataDetail(docsDetail), Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceGitea, RefreshS: 60 * 60,
		Fields:  []Field{{Key: "list_gaps", Input: InputCheck, Default: true}},
		Decode:  func(r Raw) DocsConfig { return DocsConfig{List: r.Bool("list_gaps")} },
		Queries: ownData[DocsConfig], View: dataView(docsView)}.add()
}

// docsListed caps the undocumented stacks named on the tile.
const docsListed = 12

func docsView(cfg DocsConfig, data *sources.GiteaDataset, _ ViewCtx) map[string]any {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return map[string]any{"Unread": true}
	}

	documented, total := 0, 0
	var bars []HBar
	for _, h := range check.ByHost(data.Stacks) {
		documented, total = documented+h.Documented, total+h.Total
		tier := "green"
		if h.Documented < h.Total {
			tier = "yellow"
		}
		bars = append(bars, HBar{Label: h.Host, Value: strconv.Itoa(h.Documented) + " / " + strconv.Itoa(h.Total), W: pctOf(float64(h.Documented), float64(h.Total)), Tier: tier})
	}

	var gaps []string
	for _, s := range check.Missing {
		if !cfg.List || len(gaps) == docsListed {
			break
		}
		gaps = append(gaps, s.Name)
	}
	return map[string]any{"Documented": documented, "Total": total, "Hosts": bars, "Gaps": strings.Join(gaps, ", "),
		"More": len(check.Missing) - len(gaps), "Problems": len(check.Orphans) + len(check.DeprecatedLive)}
}

// docsDetail lists what to write or fix, then the hosts' figures.
func docsDetail(cfg DocsConfig, data *sources.GiteaDataset, _ ViewCtx, results map[string]any) DetailView {
	check, ok := metrics.CheckDocs(data)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}

	tasks := Tasks{Label: T("detail.itdocs.documented"), Total: len(data.Stacks), Done: len(data.Stacks) - len(check.Missing)}
	for _, l := range check.Orphans {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.orphan", "note", l.Note.Name), Meta: l.Link, State: "bad", Action: T("detail.itdocs.open_note"), Href: l.Note.URL})
	}
	for _, l := range check.DeprecatedLive {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.deprecated", "stack", l.Stack.Name, "note", l.Note.Name), Meta: l.Stack.Host, State: "warn", Action: T("detail.itdocs.open_compose"), Href: l.Stack.URL})
	}
	for _, s := range check.Missing {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.write", "stack", s.Name), Meta: s.Host, State: "warn", Action: T("detail.itdocs.open_compose"), Href: s.URL})
	}

	var rows [][]Cell
	for _, h := range check.ByHost(data.Stacks) {
		rows = append(rows, []Cell{{Value: h.Host}, {Value: h.Documented}, {Value: h.Total}})
	}
	body := &DetailBody{Blocks: []Block{
		{Kind: BlockTasks, Data: tasks},
		{Kind: BlockTable, Label: T("detail.itdocs.hosts"), Data: Table{Head: []Text{T("detail.itdocs.host"), T("detail.itdocs.documented_n"), T("detail.itdocs.stacks")}, Rows: rows, Num: []int{1, 2}}},
	}}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// ── hansei_batches ──
//
// Hansei's status note: batches waiting for review or answers, and how
// well the vault follows its rules.

func init() {
	Tile[struct{}]{Key: "hansei_batches", Detail: dataDetail(hanseiDetail), Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceGitea, RefreshS: 15 * 60,
		Fields: []Field{}, Queries: ownData[struct{}], View: dataView(hanseiView)}.add()
}

func hanseiView(_ struct{}, data *sources.GiteaDataset, _ ViewCtx) map[string]any {
	h := data.Hansei
	if h == nil {
		return map[string]any{"Unset": true}
	}
	return map[string]any{"Status": h, "ConformityPct": pctOf(h.Conformity, 1)}
}

// hanseiDetail shows the columns, the conformity and the docs findings
// Hansei picks up next.
func hanseiDetail(_ struct{}, data *sources.GiteaDataset, _ ViewCtx, results map[string]any) DetailView {
	h := data.Hansei
	if h == nil {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}

	rows := [][]Cell{
		{{Value: T("detail.hansei.review")}, {Value: h.Review}},
		{{Value: T("detail.hansei.feedback")}, {Value: h.Feedback}},
		{{Value: T("detail.hansei.done")}, {Value: h.Done}},
		{{Value: T("detail.hansei.conformity")}, {Value: pctOf(h.Conformity, 1)}},
	}
	if check, ok := metrics.CheckDocs(data); ok {
		rows = append(rows, []Cell{{Value: T("detail.hansei.findings")}, {Value: len(check.Missing) + len(check.Orphans) + len(check.DeprecatedLive)}})
	}
	rows = append(rows, []Cell{{Value: T("detail.hansei.updated")}, {Value: dayOf(h.Updated)}})
	body := &DetailBody{Blocks: []Block{{Kind: BlockTable, Label: T("detail.hansei.columns"),
		Data: Table{Head: []Text{T("detail.hansei.what"), T("detail.hansei.count")}, Rows: rows, Num: []int{1}}}}}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)

	head := DetailHead{}
	if h.URL != "" {
		head.Actions = []DetailAction{{LabelKey: "detail.hansei.open", Href: h.URL}}
	}
	return DetailView{Head: head, Body: body}
}
