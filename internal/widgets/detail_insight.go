package widgets

// Detail dialogs of the analysis and dev tiles: KPI, chart, progress,
// table, trend, JSON APIs, Gitea, GitHub and embedded pages.

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	apiBodyMax   = 8000 // characters of a custom API's answer shown
	gitListLimit = 20
)

// unsupported is the body of a tile whose data does not fit its choice.
func unsupported() DetailView {
	return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("widget.unsupported")}}}}
}

// kpiValue formats a KPI value by its kind.
func kpiValue(k *KpiResult, v float64) any {
	switch k.Kind {
	case "money":
		return Money(v, k.Currency)
	case "percent":
		return NumU(v*percentScale, 0, "%")
	case "hours":
		return NumU(v, 1, "h")
	}
	return Num(v, 0)
}

// kpiDetail (record): the value, its comparison, its history and what it
// is made of.
func kpiDetail(cfg KpiConfig, results map[string]any, ctx ViewCtx) DetailView {
	k, _ := kpiView(cfg, results, ctx)["KPI"].(*KpiResult)
	if k == nil {
		return unsupported()
	}
	label := T("opt." + string(cfg.Metric))
	body := &DetailBody{Side: []Fact{{Label: T("detail.kpi.metric"), Value: Txt("opt." + string(cfg.Metric))}}}
	body.Facts = []Kpi{{Value: kpiValue(k, k.Value), Label: label, Tier: map[string]string{"good": "green", "bad": "red"}[k.Target]}}

	// How the value comes about, in one sentence.
	body.Blocks = append(body.Blocks, Block{Kind: BlockText, Label: T("detail.kpi.how"), Data: Txt("kpi_how." + string(cfg.Metric))})
	if k.HasDelta {
		body.Facts = append(body.Facts, changeKpi(k.Delta*percentScale, T(k.DeltaKey)))
	}
	if cfg.Target > 0 {
		body.Facts = append(body.Facts, Kpi{Value: kpiValue(k, cfg.Target), Label: T("detail.kpi.target")})
		body.Side = append(body.Side, Fact{Label: T("detail.kpi.target"), Value: kpiValue(k, cfg.Target)})
	}
	today := todayOf(ctx)
	if values := kpiSeries(cfg.Metric, results["data"], today); hasValues(values) {
		g := ColGraph(values, "s1")
		label := T("detail.kpi.months")
		g.Ticks = []any{metrics.AddMonths(today, -len(values)).Format("01/2006"), metrics.AddMonths(today, -1).Format("01/2006")}
		g.Labels = monthLabels(metrics.AddMonths(today, -1), len(values), "01/2006")
		if cfg.Metric == MetricCash {
			g = LineGraph(Series{Values: values, Class: "s1"})
			label, g.Ticks = T("detail.kpi.days"), []any{Day(today.AddDate(0, 0, 1-len(values))), Txt("detail.today")}
			g.Labels = dayLabels(today, len(values))
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: label, Hero: true, Data: g})
	}
	var rows [][]Cell
	for _, d := range k.Details {
		rows = append(rows, []Cell{{Value: d.Label}, {Value: d.Note}, {Value: Money(d.Amount, k.Currency)}})
	}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("kpi.details"),
			Data: Table{Head: []Text{T("detail.kpi.what"), T("detail.kpi.note"), T("detail.kpi.amount")}, Rows: rows, Num: []int{2}}})
	}
	return DetailView{Body: body}
}

// chartDetail (record without the facts column): the months against the
// last period, as columns and as a table.
func chartDetail(cfg ChartConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := chartView(cfg, results, ctx)
	bars, _ := view["Bars"].([]Bar)
	if len(bars) == 0 {
		return unsupported()
	}
	format := func(v float64) any { return NumU(v, 1, "h") }
	if view["Unit"] == "money" {
		format = func(v float64) any { return Money(v, "") }
	}
	prevKey, _ := view["PrevKey"].(string)
	if prevKey == "" {
		prevKey = "chart.prev"
	}
	values, prev := make([]float64, len(bars)), make([]float64, len(bars))
	sum, sumPrev := 0.0, 0.0
	var rows [][]Cell
	var labels []any
	from, partial := view["DataFrom"].(time.Time)
	for i, b := range bars {
		values[i], prev[i] = b.Value, b.Prev
		labels = append(labels, b.Label)
		sum, sumPrev = sum+b.Value, sumPrev+b.Prev
		row := []Cell{{Value: b.Label}, {Value: format(b.Value)}, {Value: format(b.Prev)}, {Value: format(b.Value - b.Prev), State: stateIf(b.Value < b.Prev, "bad")}}
		// A year-ago month before the data is unknown, not zero.
		if month, ok := metrics.ParseDay(b.Label + "-01"); partial && ok && month.AddDate(-1, 0, 0).Before(from) {
			row[2], row[3] = Cell{Value: "–"}, Cell{Value: "–"}
		}
		rows = append(rows, row)
	}
	g := Graph{Kind: GraphCols, Series: []Series{{Values: values, Class: "s1", Label: Txt("chart.this")}}, Mark: -1, Ticks: []any{bars[0].Label, bars[len(bars)-1].Label}, Labels: labels}
	if cfg.ShowPrev {
		g.Series = append(g.Series, Series{Values: prev, Class: "s3", Label: Txt(prevKey)})
	}
	if goal, found := view["Goal"].(float64); found {
		g.Goal, g.HasGoal = goal, true
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.chart.sum"), Value: format(sum)}}}
	// Last year only over a full year of data; else where the data begins.
	if partial {
		body.Line = append(body.Line, Fact{Label: T("period.data_from_label"), Value: TxtA("period.data_from", "month", from.Format("01/2006"))})
	} else {
		body.Line = append(body.Line, Fact{Label: T(prevKey), Value: format(sumPrev)})
	}
	if sumPrev > 0 && !partial {
		body.Line = append(body.Line, Fact{Label: T("detail.chart.change"), Value: NumU((sum-sumPrev)/sumPrev*percentScale, 1, "%"), State: stateIf(sum < sumPrev, "bad")})
	}
	body.Blocks = []Block{{Kind: BlockGraph, Label: T("chart." + string(cfg.Chart)), Hero: true, Data: g},
		{Kind: BlockTable, Label: T("detail.chart.months"), Data: Table{Head: []Text{T("detail.chart.month"), T("chart.this"), T(prevKey), T("detail.chart.diff")}, Rows: rows, Num: []int{1, 2, 3}}}}
	return DetailView{Body: body}
}

// progressDetail (record): every meter with where it should be today.
func progressDetail(cfg ProgressConfig, results map[string]any, ctx ViewCtx) DetailView {
	items, _ := progressView(cfg, results, ctx)["Items"].([]ProgressItem)
	if len(items) == 0 {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.progress.none")}}}}
	}
	var bars []ShareBar
	var rows [][]Cell
	over, behind := 0, 0
	for _, it := range items {
		name := any(it.Label)
		if it.LabelKey != "" {
			name = Txt(it.LabelKey)
		}
		bars = append(bars, ShareBar{Name: name, Pct: min(it.Pct*percentScale, percentScale), Value: NumU(it.Pct*percentScale, 0, "%"), Tier: it.Tier})
		soll := any("–")
		if it.SollPct > 0 {
			soll = NumU(it.SollPct*percentScale, 0, "%")
		}
		rows = append(rows, []Cell{{Value: name}, {Value: NumU(it.Pct*percentScale, 0, "%"), State: tierState(it.Tier)}, {Value: soll}})
		switch it.Tier {
		case "red":
			over++
		case "yellow":
			behind++
		}
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.progress.meters"), Value: len(items)}, {Label: T("detail.progress.over"), Value: over, State: stateIf(over > 0, "bad")},
			{Label: T("detail.progress.off_track"), Value: behind, State: stateIf(behind > 0, "warn")}},
		Facts: []Kpi{{Value: len(items), Label: T("detail.progress.meters")}, {Value: over, Label: T("detail.progress.over"), Tier: tierIf(over > 0, "red", "")},
			{Value: behind, Label: T("detail.progress.off_track"), Tier: tierIf(behind > 0, "yellow", "")}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.progress.state"), Data: bars},
			{Kind: BlockTable, Label: T("detail.progress.list"), Data: Table{Head: []Text{T("detail.progress.name"), T("detail.progress.used"), T("detail.progress.should")}, Rows: rows, Num: []int{1, 2}}}},
	}

	// Budget use over the last weeks, one line per project (recorded daily).
	now := time.Now()
	var lines []Series
	for _, it := range items {
		if it.Series == "" || len(lines) == progressLines {
			continue
		}
		if values := dailySeries(historyOf(results), it.Series, now, progressDays); hasValues(values) {
			lines = append(lines, Series{Values: scaled(values, percentScale), Class: dataClass(len(lines)), Label: it.Label})
		}
	}
	if len(lines) > 0 {
		g := LineGraph(lines...)
		g.Goal, g.HasGoal, g.GoalDanger, g.Ticks = percentScale, true, true, spanTicks(now, progressDays)
		g.Labels = dayLabels(now, progressDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.progress.history"), Meta: "%", Data: g})
	}
	return DetailView{Body: body}
}

// progressDays is the span of the budget lines, at most progressLines.
const (
	progressDays  = 60
	progressLines = 6 // Kante has six data colours
)

// tableCell formats a table tile's value as its column says.
func tableCell(format string, v any) Cell {
	switch format {
	case "money":
		return Cell{Value: Money(asF(v), "")}
	case "pct", "bar":
		return Cell{Value: NumU(asF(v)*percentScale, 0, "%"), State: stateIf(format == "bar" && asF(v) >= 1, "bad")}
	case "hours":
		return Cell{Value: NumU(asF(v), 1, "h")}
	case "km":
		return Cell{Value: NumU(asF(v), 0, "km")}
	case "day":
		if s, ok := v.(string); ok && s != "" {
			return Cell{Value: DayS(s)}
		}
		return Cell{Value: "–"}
	case "daycount":
		return Cell{Value: TxtA("col.days", "days", v)}
	case "late":
		if asF(v) > 0 {
			return Cell{Value: TxtA("col.days", "days", v), State: "bad"}
		}
		return Cell{Value: "–"}
	case "upcoming", "match":
		return Cell{Value: Txt(fmt.Sprintf("%s.%v", format, v))}
	case "yesno":
		if b, _ := v.(bool); b {
			return Cell{Value: Txt("detail.table.answer_yes")}
		}
		return Cell{Value: Txt("detail.table.answer_no"), State: "bad"}
	case "risk":
		n := int(asF(v))
		return Cell{Value: Txt(fmt.Sprintf("risk.%d", n)), State: []string{"ok", "warn", "bad"}[min(max(n, 0), 2)]}
	}
	if v == nil || v == "" {
		return Cell{Value: "–"}
	}
	return Cell{Value: v}
}

// tableDetail (list and detail): every row, the first in detail.
func tableDetail(cfg TableConfig, results map[string]any, ctx ViewCtx) DetailView {
	full := cfg
	full.Limit = int(^uint(0) >> 1)
	view := tableView(full, results, ctx)
	cols, _ := view["Cols"].([]Col)
	rows, _ := view["Rows"].([]Row)
	if len(cols) == 0 {
		return unsupported()
	}
	head := make([]Text, len(cols))
	var num []int
	for i, c := range cols {
		head[i] = T("col." + c.Label)
		if c.Numeric() || c.Format == "pct" {
			num = append(num, i)
		}
	}
	var cells [][]Cell
	var keys []string
	list := &ObjList{Label: textArgs("detail.table.rows", "n", len(rows))}
	for n, r := range rows {
		line := make([]Cell, len(cols))
		for i, c := range cols {
			line[i] = tableCell(c.Format, r.Values[i])
		}
		cells = append(cells, line)
		keys = append(keys, strconv.Itoa(n))
		item := LitRow{Name: line[0].Value, State: "info", Item: keys[n]}
		if len(num) > 0 {
			item.Meta = line[num[len(num)-1]].Value
		}
		list.Items = append(list.Items, item)
	}
	table := Table{Head: head, Rows: cells, Num: num}
	if sum, found := view["Sum"].(Row); found {
		for i, c := range cols {
			table.Foot = append(table.Foot, tableCell(c.Format, sum.Values[i]))
		}
	}
	body := &DetailBody{}
	if len(cells) > 0 {
		// A clicked row shows all its columns.
		list.Sel = pickIndex(results, keys)
		list.Title = cells[list.Sel][0].Value
		body.List = list
		var facts [][]Cell
		for i := range cols {
			facts = append(facts, []Cell{{Value: Txt("col." + cols[i].Label)}, cells[list.Sel][i]})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: facts}})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("opt." + string(cfg.Table)), Data: table})
	return DetailView{Body: body}
}

// trendDetail (record without the facts column): the series with its
// target, now against the start, low and high.
func trendDetail(cfg TrendConfig, results map[string]any, _ ViewCtx) DetailView {
	points, _ := results["points"].([][2]any)
	if len(points) < minPoints {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.series.few")}}}}
	}
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = asF(p[1])
		if cfg.Metric == TrendMonthMinute {
			values[i] /= minutesPerHourInsight
		}
	}
	format := func(v float64) any { return Money(v, "") }
	if cfg.Metric == TrendMonthMinute {
		format = func(v float64) any { return NumU(v, 1, "h") }
	}
	first, last := values[0], values[len(values)-1]
	low, high := slices.Min(values), slices.Max(values)
	g := LineGraph(Series{Values: values, Class: "s1"})
	g.Ticks = []any{DayS(fmt.Sprint(points[0][0])), DayS(fmt.Sprint(points[len(points)-1][0]))}
	for _, p := range points {
		g.Labels = append(g.Labels, DayS(fmt.Sprint(p[0])))
	}
	if cfg.Target > 0 {
		g.Goal, g.HasGoal = cfg.Target, true
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.series.now"), Value: format(last)}, {Label: T("detail.series.start"), Value: format(first)},
		{Label: T("detail.series.low"), Value: format(low)}, {Label: T("detail.series.high"), Value: format(high)}},
		Blocks: []Block{{Kind: BlockGraph, Label: T("opt." + string(cfg.Metric)), Hero: true, Data: g}}}
	if cfg.Target > 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.kpi.target"), Value: format(cfg.Target)})
	}

	// The biggest jumps, explained by the invoices and payments of their days.
	if ninja, ok := results[peerNinja].(*sources.NinjaDataset); ok {
		var days []string
		for _, i := range metrics.Jumps(values, trendJumps) {
			days = append(days, fmt.Sprint(points[i][0]))
		}
		var rows [][]Cell
		for _, m := range metrics.NinjaMoves(ninja, days) {
			what := any(m.What)
			if m.What == "" {
				what = Txt("detail.series.payment")
			}
			rows = append(rows, []Cell{{Value: DayS(m.Day)}, {Value: what}, {Value: m.Client}, {Value: Money(m.Amount, ninja.Currency), State: stateIf(m.Amount < 0, "ok")}})
		}
		if len(rows) > 0 {
			body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.series.moves"),
				Data: Table{Head: []Text{T("detail.series.day"), T("detail.series.what"), T("detail.series.client"), T("detail.series.amount")}, Rows: rows, Num: []int{3}}})
		}
	}
	return DetailView{Body: body}
}

// jsonSparkDays is the span of a JSON field's line.
const jsonSparkDays = 30

// trendJumps is how many jumps the trend dialog explains.
const trendJumps = 3

// levelTier maps a threshold level to a card tier.
func levelTier(level string) string {
	return map[string]string{levelWarn: "yellow", levelFail: "red"}[level]
}

// jsonAPIDetail (wall): the key figures as cards, the list under them.
func jsonAPIDetail(cfg JSONAPIConfig, data *sources.JSONAPIDataset, ctx ViewCtx, results map[string]any) DetailView {
	fields, _ := jsonAPIView(cfg, data, ctx)["Fields"].([]JSONFieldView)
	now := time.Now()
	var cards []Card
	for _, f := range fields {
		card := Card{Label: Plain(f.Label), Value: f.Text, Tier: levelTier(f.Level), Sub: f.Path}
		switch {
		case !f.Found:
			card.Value, card.Tier = "–", "red"
		case f.Numeric:
			card.Value = NumU(f.Value, 2, f.Unit)

			// The field's last month, recorded daily.
			if past := dailySeries(historyOf(results), metrics.JSONFieldKey(data.Host, f.Label), now, jsonSparkDays); hasValues(past) {
				card.Spark = filled(past)
			}
		}
		cards = append(cards, card)
	}
	body := &DetailBody{}
	if len(cards) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockWall, Data: cards})
	}
	if len(data.Columns) > 0 {
		head := make([]Text, len(data.Columns))
		for i, c := range data.Columns {
			head[i] = Plain(c)
		}
		var rows [][]Cell
		for _, r := range data.Rows {
			line := make([]Cell, len(r))
			for i, v := range r {
				line[i] = Cell{Value: v}
			}
			rows = append(rows, line)
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.api.list"), Data: Table{Head: head, Rows: rows}})
	}
	if len(body.Blocks) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.api.none")}}
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// customAPIDetail (tabs): the picked values, the whole answer.
func customAPIDetail(cfg CustomAPIConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["body"].(*sources.JSONResult)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.api.none")}}}}
	}
	values, _ := customAPIView(cfg, results, ctx)["Rows"].([]APIValue)
	var cards []Card
	var rows [][]Cell
	for i, v := range values {
		card := Card{Label: Plain(v.Label), Value: v.Value + v.Unit, Tier: levelTier(v.Level), Sub: cfg.Fields[i].Path}
		if v.Missing {
			card.Value, card.Tier = "–", "red"
		}
		cards = append(cards, card)
		rows = append(rows, []Cell{{Value: v.Label}, {Value: cfg.Fields[i].Path}, {Value: card.Value, State: tierState(card.Tier)}})
	}
	answer, _ := json.MarshalIndent(data.Body, "", "  ")
	text := string(answer)
	if len(text) > apiBodyMax {
		text = text[:apiBodyMax] + "\n…"
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.api.url"), Value: cfg.URL}},
		Tabs: []Tab{{Label: T("detail.api.values"), Count: len(values), Blocks: []Block{{Kind: BlockWall, Data: cards},
			{Kind: BlockTable, Data: Table{Head: []Text{T("detail.api.field"), T("detail.api.path"), T("detail.api.value")}, Rows: rows}}}},
			{Label: T("detail.api.answer"), Blocks: []Block{{Kind: BlockCode, Data: text}}}}}

	// Every value of the answer not shown yet, to add as a field by click.
	var add []Task
	for _, path := range scalarPaths(data.Body, "", apiPathsShown, nil) {
		if slices.ContainsFunc(cfg.Fields, func(f APIField) bool { return f.Path == path }) {
			continue
		}
		v, _ := jsonPath(data.Body, path)
		add = append(add, Task{Text: path, Meta: textOf(v), State: "info", Action: T("detail.api.add"), Do: "add_field", Args: map[string]string{"path": path}})
	}
	if len(add) > 0 {
		body.Tabs = append(body.Tabs, Tab{Label: T("detail.api.paths"), Count: len(add), Blocks: []Block{{Kind: BlockTasks, Data: Tasks{Items: add}}}})
	}
	return DetailView{Body: body}
}

// apiPathsShown caps the paths offered as new fields.
const apiPathsShown = 40

// issueMarks are the rule limits an issue table marks: waiting this many
// days (warn), and due within dueWarn days (warn) or past (bad). A table
// with dueWarn 0 has no due column.
type issueMarks struct{ wait, dueWarn float64 }

// issueRows lists issues or pull requests.
func issueRows(list []sources.Issue, marks issueMarks, today time.Time) [][]Cell {
	var rows [][]Cell
	for _, it := range firstN(list, gitListLimit) {
		waited := today.Sub(it.Updated).Hours() / hoursPerDay
		row := []Cell{{Value: it.Repo}, {Value: fmt.Sprintf("#%d", it.Number)}, {Value: it.Title},
			{Value: agoOf(it.Updated), State: stateIf(marks.wait > 0 && waited >= marks.wait, "warn")}}
		if marks.dueWarn > 0 {
			row = append(row, Cell{Value: dayOf(it.Due), State: dueState(it.Due, today, marks.dueWarn)})
		}
		rows = append(rows, row)
	}
	return rows
}

// dueState: bad when past, warn within warn days, "" otherwise or undated.
func dueState(due, today time.Time, warn float64) string {
	switch {
	case due.IsZero():
		return ""
	case due.Before(today):
		return "bad"
	case due.Sub(today).Hours()/hoursPerDay <= warn:
		return "warn"
	}
	return ""
}

// issueHead is the head of an issue table.
func issueHead(marks issueMarks) []Text {
	head := []Text{T("detail.git.repo"), T("detail.git.number"), T("detail.git.title"), T("detail.git.updated")}
	if marks.dueWarn > 0 {
		head = append(head, T("detail.git.due"))
	}
	return head
}

// giteaDetail (record): reviews waiting, assigned issues, failed runs.
func giteaDetail(cfg PickConfig, data *sources.GiteaDataset, ctx ViewCtx, results map[string]any) DetailView {
	today := todayOf(ctx)
	reviews := issueMarks{wait: rules.Setting(ctx.Settings, "gitea.review_waiting", "days")}
	assigned := issueMarks{dueWarn: rules.Setting(ctx.Settings, "gitea.due", "warn_days")}
	var failed []LitRow
	for _, r := range data.Repos {
		if r.FailedWorkflow != "" {
			failed = append(failed, LitRow{Name: r.Name, Meta: r.FailedWorkflow, State: "bad"})
		}
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.git.user"), Value: data.User}, {Label: T("detail.git.notifications"), Value: data.Notifications},
			{Label: T("detail.git.repos"), Value: len(data.Repos)}},
		Facts: []Kpi{{Value: len(data.Reviews), Label: T("gitea.reviews"), Tier: tierIf(len(data.Reviews) > 0, "yellow", "")}, {Value: len(data.Assigned), Label: T("gitea.assigned")},
			{Value: len(failed), Label: T("detail.git.failed"), Tier: tierIf(len(failed) > 0, "red", "")}},
	}
	if cfg.Only != "issues" && len(data.Reviews) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("gitea.reviews"), Data: Table{Head: issueHead(reviews), Rows: issueRows(data.Reviews, reviews, today)}})
	}
	if cfg.Only != "reviews" && len(data.Assigned) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("gitea.assigned"), Data: Table{Head: issueHead(assigned), Rows: issueRows(data.Assigned, assigned, today)}})
	}
	if len(failed) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.git.failed_runs"), Data: failed})
	}
	if act, ok := results[openName].(*sources.GiteaActivity); ok {
		body.Blocks = append(body.Blocks, giteaActivity(act)...)
	}
	if len(body.Blocks) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.git.calm")}}
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// giteaActivity: commits per week of the active repos as small columns,
// the push mirrors with their last sync (late after mirrorLate).
func giteaActivity(act *sources.GiteaActivity) []Block {
	var out []Block
	var cards []Card
	for _, name := range slices.Sorted(maps.Keys(act.Weeks)) {
		weeks := act.Weeks[name]
		total := 0
		spark := make([]float64, len(weeks))
		for i, n := range weeks {
			spark[i], total = float64(n), total+n
		}
		cards = append(cards, Card{Label: Plain(name), Value: total, Spark: spark, Sub: Txt("detail.git.commits_8w")})
	}
	if len(cards) > 0 {
		out = append(out, Block{Kind: BlockWall, Label: T("detail.git.activity"), Data: cards})
	}
	var mirrors [][]Cell
	for _, m := range act.Mirrors {
		state := ""
		switch {
		case m.Error != "":
			state = "bad"
		case time.Since(m.Synced) > mirrorLate:
			state = "warn"
		}
		mirrors = append(mirrors, []Cell{{Value: m.Repo}, {Value: m.Remote}, {Value: agoOf(m.Synced), State: state}, {Value: cmp.Or(m.Error, "–")}})
	}
	if len(mirrors) > 0 {
		out = append(out, Block{Kind: BlockTable, Label: T("detail.git.mirrors"),
			Data: Table{Head: []Text{T("detail.git.repo"), T("detail.git.remote"), T("detail.git.synced"), T("detail.git.error")}, Rows: mirrors}})
	}
	return out
}

// mirrorLate: a push mirror syncs on every push and every 8 h by default.
const mirrorLate = 24 * time.Hour

// ciStates maps a GitHub run conclusion to a state.
var ciStates = map[string]string{"success": "ok", "failure": "bad", "cancelled": "warn", "timed_out": "bad"}

// githubDetail (record): repos with CI and release, reviews, own PRs.
func githubDetail(cfg GitHubConfig, data *sources.GitHubDataset, ctx ViewCtx, results map[string]any) DetailView {
	today := todayOf(ctx)
	reviews := issueMarks{wait: rules.Setting(ctx.Settings, "github.review_waiting", "days")}
	mine := issueMarks{wait: rules.Setting(ctx.Settings, "github.stale_pr", "days")}
	shown, _ := githubView(cfg, data, ctx)["Data"].(*sources.GitHubDataset)
	red := 0
	var repos [][]Cell
	for _, r := range shown.Repos {
		if metrics.RedCI(r) {
			red++
		}
		release := any("–")
		if r.Release != "" {
			release = TxtA("detail.git.release", "name", r.Release, "day", Day(r.ReleasedAt))
		}
		ci := Cell{Value: cmp.Or(r.CI, "–"), State: ciStates[r.CI], Href: r.CIURL}
		if r.CIStep != "" {
			ci.Value = r.CIStep
		}
		pushed := any("–")
		if !r.PushedAt.IsZero() {
			pushed = agoOf(r.PushedAt)
		}
		repos = append(repos, []Cell{{Value: r.Name}, {Value: r.Issues}, {Value: r.PRs}, ci, {Value: pushed}, {Value: release}})
	}
	body := &DetailBody{
		Side: []Fact{{Label: T("detail.git.repos"), Value: len(shown.Repos)}, {Label: T("detail.git.notifications"), Value: shown.Notifications}},
		Facts: []Kpi{{Value: red, Label: T("detail.git.ci_red"), Tier: tierIf(red > 0, "red", "")}, {Value: len(shown.Reviews), Label: T("detail.git.reviews"), Tier: tierIf(len(shown.Reviews) > 0, "yellow", "")},
			{Value: len(shown.MyPRs), Label: T("detail.git.mine")}},
		Blocks: []Block{{Kind: BlockTable, Label: T("detail.git.repos"), Data: Table{Head: []Text{T("detail.git.repo"), T("detail.git.issues"), T("detail.git.prs"), T("detail.git.ci"), T("detail.git.pushed"), T("detail.git.latest")},
			Rows: repos, Num: []int{1, 2}}}},
	}
	if len(shown.Reviews) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.git.reviews"), Data: Table{Head: issueHead(reviews), Rows: issueRows(shown.Reviews, reviews, today)}})
	}
	if len(shown.MyPRs) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.git.mine"), Data: Table{Head: issueHead(mine), Rows: issueRows(shown.MyPRs, mine, today)}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: "https://github.com/notifications", Primary: true}}}, Body: body}
}

// iframeDetail (large view): the page at dialog size.
func iframeDetail(cfg IframeConfig, _ map[string]any, _ ViewCtx) DetailView {
	head := DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: cfg.URL, Primary: true}}}
	return DetailView{Head: head, Body: &DetailBody{Blocks: []Block{{Kind: BlockFrame, Data: Embed{URL: cfg.URL}}}}}
}
