package widgets

// ── docs_coverage ──
//
// How much of the compose stacks the IT docs cover, per host, and the
// stacks still without a note (Phase 15, rules docs.*). With a Komodo in
// the space also what it does not run, or runs outside the repos.

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
		Queries: func(DocsConfig) []Query { return append(dataQuery(nil), komodoPeer) }, View: docsView}.add()
}

// komodoPeer is the space's Komodo, for what runs (docs.not_deployed,
// docs.deployed_unknown).
var komodoPeer = peer(string(enums.ServiceKomodo), enums.ServiceKomodo)

// deploysOf compares the stacks with the space's Komodo; ok is false
// without one.
func deploysOf(data *sources.GiteaDataset, results map[string]any) (metrics.DeployCheck, bool) {
	komodo, _ := results[string(enums.ServiceKomodo)].(*sources.KomodoDataset)
	return metrics.CheckDeploys(data, komodo)
}

// docsListed caps the undocumented stacks named on the tile.
const docsListed = 12

func docsView(cfg DocsConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results[dataName].(*sources.GiteaDataset)
	if !ok {
		return map[string]any{}
	}
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
	view := map[string]any{"Documented": documented, "Total": total, "Hosts": bars, "Gaps": strings.Join(gaps, ", "),
		"More": len(check.Missing) - len(gaps), "Problems": len(check.Orphans) + len(check.DeprecatedLive)}
	if deploys, ok := deploysOf(data, results); ok && len(deploys.NotDeployed)+len(deploys.Unknown) > 0 {
		view["NotDeployed"], view["Unknown"] = len(deploys.NotDeployed), len(deploys.Unknown)
	}
	if drifts := driftsOf(data, results); len(drifts) > 0 {
		view["Drifts"] = len(drifts)
	}
	return view
}

// driftsOf are the notes that differ from their stacks or from what the
// space's Komodo runs.
func driftsOf(data *sources.GiteaDataset, results map[string]any) []metrics.Drift {
	komodo, _ := results[string(enums.ServiceKomodo)].(*sources.KomodoDataset)
	drifts, _ := metrics.CheckDrift(data, komodo)
	return drifts
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
	for _, d := range driftsOf(data, results) {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.drift", "note", d.Note.Name), Meta: d.Text(), State: "warn", Action: T("detail.itdocs.open_note"), Href: d.Note.URL})
	}
	for _, s := range check.Missing {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.write", "stack", s.Name), Meta: s.Host, State: "warn", Action: T("detail.itdocs.open_compose"), Href: s.URL})
	}

	var rows [][]Cell
	for _, h := range check.ByHost(data.Stacks) {
		rows = append(rows, []Cell{{Value: h.Host}, {Value: h.Documented}, {Value: h.Total}})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockTasks, Data: tasks}}}
	if deploys, ok := deploysOf(data, results); ok {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTasks, Data: deployTasks(deploys, results)})
	}
	body.Blocks = append(body.Blocks,
		Block{Kind: BlockTable, Label: T("detail.itdocs.hosts"), Data: Table{Head: []Text{T("detail.itdocs.host"), T("detail.itdocs.documented_n"), T("detail.itdocs.stacks")}, Rows: rows, Num: []int{1, 2}}})
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// deployTasks lists the stacks Komodo does not run and those it runs
// outside the repos.
func deployTasks(check metrics.DeployCheck, results map[string]any) Tasks {
	komodo, _ := results[string(enums.ServiceKomodo)].(*sources.KomodoDataset)
	tasks := Tasks{Label: T("detail.itdocs.deployed"), Total: check.Compared, Done: check.Compared - len(check.NotDeployed)}
	for _, d := range check.Unknown {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.unknown", "stack", d.Stack.Name), Meta: d.Host, State: "bad", Action: T("detail.itdocs.open_komodo"), Href: komodo.URL})
	}
	for _, s := range check.NotDeployed {
		tasks.Items = append(tasks.Items, Task{Text: TxtA("detail.itdocs.not_deployed", "stack", s.Name), Meta: s.Host, State: "warn", Action: T("detail.itdocs.open_compose"), Href: s.URL})
	}
	return tasks
}

// ── hansei_batches ──
//
// Hansei's pushed state: batches waiting for review or answers, how well
// the vault follows its rules, and the docs findings a batch works on.

func init() {
	Tile[struct{}]{Key: "hansei_batches", Detail: dataDetail(hanseiDetail), Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceHansei, RefreshS: 5 * 60,
		Fields: []Field{}, Queries: ownData[struct{}], View: dataView(hanseiView)}.add()
}

func hanseiView(_ struct{}, data *sources.HanseiDataset, _ ViewCtx) map[string]any {
	if data.Updated.IsZero() {
		return map[string]any{"Unset": true}
	}
	return map[string]any{"Status": data, "ConformityPct": pctOf(data.Conformity, 1)}
}

// hanseiDetail shows the columns, the conformity and the claimed findings.
func hanseiDetail(_ struct{}, data *sources.HanseiDataset, _ ViewCtx, results map[string]any) DetailView {
	if data.Updated.IsZero() {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}

	rows := [][]Cell{
		{{Value: T("detail.hansei.review")}, {Value: data.Review}},
		{{Value: T("detail.hansei.feedback")}, {Value: data.Feedback}},
		{{Value: T("detail.hansei.done")}, {Value: data.Done}},
		{{Value: T("detail.hansei.conformity")}, {Value: pctOf(data.Conformity, 1)}},
		{{Value: T("detail.hansei.claimed")}, {Value: len(data.Claimed)}},
		{{Value: T("detail.hansei.updated")}, {Value: dayOf(data.Updated)}},
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockTable, Label: T("detail.hansei.columns"),
		Data: Table{Head: []Text{T("detail.hansei.what"), T("detail.hansei.count")}, Rows: rows, Num: []int{1}}}}}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
