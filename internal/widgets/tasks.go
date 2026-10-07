package widgets

// "vikunja_tasks": open tasks by due date, overdue ones first; the dialog
// lists every open task and the open ones per project.

import (
	"sort"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TasksConfig is the "vikunja_tasks" widget's config.
type TasksConfig struct{ Limit int }

const tasksShown = 6

func init() {
	Tile[TasksConfig]{Key: "vikunja_tasks", Detail: dataDetail(tasksDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceVikunja, RefreshS: 600,
		Fields:  []Field{{Key: "limit", Input: InputNumber, Default: tasksShown, Min: "1", Max: "20"}},
		Decode:  func(r Raw) TasksConfig { return TasksConfig{Limit: r.Int("limit")} },
		Queries: ownData[TasksConfig], View: dataView(tasksView),
		Calm: func(v map[string]any) bool { return v["Overdue"] == 0 }}.add()
}

// TaskLine is an open task with whether it is late.
type TaskLine struct {
	sources.VikunjaTask
	Late bool
}

// openByDue sorts open tasks: with a date first, soonest first.
func openByDue(data *sources.VikunjaDataset, today time.Time) []TaskLine {
	var out []TaskLine
	for _, t := range data.Open() {
		out = append(out, TaskLine{VikunjaTask: t, Late: !t.Due.IsZero() && t.Due.Before(today)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Due, out[j].Due
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return a.Before(b)
	})
	return out
}

func tasksView(cfg TasksConfig, data *sources.VikunjaDataset, ctx ViewCtx) map[string]any {
	lines := openByDue(data, todayOf(ctx))
	overdue := 0
	for _, l := range lines {
		if l.Late {
			overdue++
		}
	}
	return map[string]any{"Open": len(lines), "Overdue": overdue, "Lines": firstN(lines, cfg.Limit)}
}

func tasksDetail(_ TasksConfig, data *sources.VikunjaDataset, ctx ViewCtx, results map[string]any) DetailView {
	lines := openByDue(data, todayOf(ctx))
	perProject := map[string]int{}
	var rows [][]Cell
	overdue := 0
	for _, l := range lines {
		perProject[l.Project]++
		due := any("–")
		if !l.Due.IsZero() {
			due = Day(l.Due)
		}
		if l.Late {
			overdue++
		}
		rows = append(rows, []Cell{{Value: l.Title}, {Value: orNone(l.Project)}, {Value: due, State: stateIf(l.Late, "bad")}, {Value: l.Priority}})
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(lines), Label: T("tasks.open")}, {Value: overdue, Label: T("tasks.overdue"), Tier: tierIf(overdue > 0, "red", "green")}}}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("tasks.list"), Data: Table{
			Head: []Text{T("tasks.col.title"), T("tasks.col.project"), T("tasks.col.due"), T("tasks.col.priority")}, Rows: rows, Num: []int{3}}})
		top := 1
		for _, n := range perProject {
			top = max(top, n)
		}
		var bars []ShareBar
		for name, n := range perProject {
			bars = append(bars, ShareBar{Name: orNone(name), Pct: float64(n) / float64(top) * percentScale, Value: n})
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].Pct > bars[j].Pct })
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("tasks.per_project"), Data: bars})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
