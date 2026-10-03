package widgets

// Detail dialogs of the overview tiles: links, connections, deadlines,
// hints, timeline, today, week story, calendar, clock.

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	calendarDetailDays = 14
	recentDetailMax    = 30
	freeFrom, freeTo   = 9, 18 // working hours checked for free time
	freeMin            = 2     // hours; shorter gaps are no free time
)

// linksDownDetail (record without facts column): every link without an
// answer since when.
func linksDownDetail(_ LinksDownConfig, results map[string]any, ctx ViewCtx) DetailView {
	links, _ := results[LinksDownSlot].([]DownLink)
	sort.SliceStable(links, func(i, j int) bool { return downSince(links[i]).Before(downSince(links[j])) })
	today := todayOf(ctx)
	var rows [][]Cell
	longest := 0
	for _, l := range links {
		since := any(Txt("detail.links.today"))
		if !l.Since.IsZero() {
			days := int(today.Sub(l.Since).Hours()/hoursPerDay) + 1
			longest = max(longest, days)
			since = TxtA("detail.links.since", "day", Day(l.Since), "n", days)
		}
		rows = append(rows, []Cell{{Value: l.Title}, {Value: l.URL}, {Value: since, State: "bad"}, {Value: cmp.Or(l.Cause, "–")}})
	}
	// Several links with one cause are one problem: a host or a proxy.
	byCause := map[string]int{}
	for _, l := range links {
		byCause[cmp.Or(l.Cause, "–")]++
	}
	causes := slices.Collect(maps.Keys(byCause))
	sort.Slice(causes, func(a, b int) bool { return byCause[causes[a]] > byCause[causes[b]] })
	var groups []ShareBar
	for _, c := range causes {
		groups = append(groups, ShareBar{Name: c, Pct: float64(byCause[c]) * percentScale / float64(max(len(links), 1)), Value: byCause[c], Tier: "red"})
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(links), Label: T("detail.links.down"), Tier: tierIf(len(links) > 0, "red", "green")}}}
	if len(links) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.links.all_up")}}
		return DetailView{Body: body}
	}
	body.Facts = append(body.Facts, Kpi{Value: TxtA("detail.days", "n", longest), Label: T("detail.links.longest")})
	body.Blocks = []Block{{Kind: BlockBars, Label: T("detail.links.causes"), Data: groups},
		{Kind: BlockTable, Label: T("detail.links.list"), Data: Table{Head: []Text{T("detail.links.title"), T("detail.links.url"), T("detail.links.since_label"), T("detail.links.cause")}, Rows: rows}}}
	return DetailView{Head: DetailHead{State: "bad", StateKey: "detail.links.n_down", StateArgs: map[string]any{"n": len(links)}}, Body: body}
}

// connHealthDetail (record without facts column): every connection's
// days as a strip, failing ones first.
func connHealthDetail(_ ConnHealthConfig, results map[string]any, ctx ViewCtx) DetailView {
	strips, _ := results[ConnHealthSlot].([]ConnStrip)
	sort.SliceStable(strips, func(a, b int) bool { return strips[a].FailPct > strips[b].FailPct })
	healthy := 0
	var rows []Strip
	for _, s := range strips {
		if s.FailPct == 0 {
			healthy++
		}
		row := Strip{Name: s.Name, Value: NumU(float64(100-s.FailPct), 0, "%")}
		for _, d := range s.Days {
			row.States = append(row.States, map[string]string{"ok": "ok", "mid": "warn", "bad": "bad", "none": "off"}[cellState(d)])
		}
		rows = append(rows, row)
	}
	body := &DetailBody{Facts: []Kpi{{Value: fmt.Sprintf("%d / %d", healthy, len(strips)), Label: T("detail.conn.healthy"), Tier: tierIf(healthy < len(strips), "yellow", "green")}}}
	ticks := spanTicks(todayOf(ctx), ConnHealthDays)
	var open []LitRow
	for _, s := range strips {
		id := strconv.FormatInt(s.ID, 10)
		open = append(open, LitRow{Name: s.Name, Meta: s.Service, State: stateIf(s.FailPct > 0, "warn"), Item: id})
		if id != pickedItem(results) {
			continue
		}
		// One connection picked: its days as fetches, its error, its tiles.
		okDays, failDays := make([]float64, len(s.Days)), make([]float64, len(s.Days))
		for i, d := range s.Days {
			okDays[i], failDays[i] = float64(d.OK), float64(d.Fail)
		}
		g := LineGraph(Series{Values: okDays, Class: "s1", Label: Txt("detail.conn.ok")}, Series{Values: failDays, Class: "s2", Label: Txt("detail.conn.failed")})
		g.Lo, g.Ticks = 0, ticks
		facts := Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: [][]Cell{
			{{Value: Txt("detail.conn.service")}, {Value: s.Service}},
			{{Value: Txt("detail.conn.last_error")}, {Value: cmp.Or(s.LastError, "–"), State: stateIf(s.LastError != "", "warn")}},
			{{Value: Txt("detail.conn.avg_ms")}, {Value: NumU(float64(s.AvgMs), 0, "ms")}},
			{{Value: Txt("detail.conn.tiles")}, {Value: s.Tiles}}}}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: Plain(s.Name), Data: facts}, Block{Kind: BlockGraph, Label: T("detail.conn.fetches"), Hero: true, Data: g})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockStrips, Label: T("detail.conn.days"), Ticks: ticks, Data: rows},
		Block{Kind: BlockRows, Label: T("detail.conn.open"), Data: open})
	return DetailView{Body: body}
}

// deadlinesDetail (record without facts column): the tax deadlines with
// their amounts.
func deadlinesDetail(cfg DeadlinesConfig, results map[string]any, ctx ViewCtx) DetailView {
	tax, configured := metrics.ParseTaxSettings(ctx.Settings)
	if !configured {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.deadlines.setup")}}}}
	}
	today := todayOf(ctx)
	var events []Event
	taxWarn, taxNotice := rules.Setting(ctx.Settings, "tax.deadlines", "warn_days"), rules.Setting(ctx.Settings, "tax.deadlines", "notice_days")
	for _, d := range metrics.UpcomingDeadlines(tax, today, max(cfg.Days, deadlineDetailDays)) {
		left := int(d.Due.Sub(today).Hours() / hoursPerDay)
		state := any(TxtA("detail.days", "n", left))
		if d.Amount != nil {
			state = Money(*d.Amount, "EUR")
		}
		events = append(events, Event{At: d.Due, Title: TxtA("deadline."+d.Kind, "period", d.Period, "year", d.Year), Sub: TxtA("detail.deadlines.in", "n", left),
			State: state, Tier: cmp.Or(dueTier(left, taxWarn, taxNotice), "cyan")})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockTimeline, Label: T("detail.deadlines.list"), Data: events}}}
	if len(events) > 0 {
		body.Facts = []Kpi{{Value: Day(events[0].At), Label: T("detail.deadlines.next")}, {Value: len(events), Label: T("detail.deadlines.count")}}
	}

	// Hand in: tick a deadline of the next weeks once it is filed.
	filed := map[string]time.Time{}
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			if e.Kind == metrics.EventFiled {
				filed[e.Subject] = e.At
			}
		}
	}
	tasks := Tasks{Label: T("detail.deadlines.filed")}
	for _, d := range metrics.UpcomingDeadlines(tax, today, deadlineTickDays) {
		key := metrics.DeadlineKey(d.Kind, d.Period, d.Year)
		t := Task{Text: TxtA("deadline."+d.Kind, "period", d.Period, "year", d.Year), Meta: Day(d.Due), State: "warn",
			Action: T("detail.deadlines.mark_filed"), Do: "deadline_filed", Args: map[string]string{"key": key}}
		if at, ok := filed[key]; ok {
			t.State, t.Meta, t.Do, tasks.Done = "ok", TxtA("detail.deadlines.filed_on", "day", Day(at)), "", tasks.Done+1
		}
		tasks.Total++
		tasks.Items = append(tasks.Items, t)
	}
	if tasks.Total > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTasks, Data: tasks})
	}
	return DetailView{Body: body}
}

// deadlineDetailDays is how far ahead the dialog looks: a year.
const deadlineDetailDays = 365

// expiriesDetail (timeline): every hint with a due date on one line.
func expiriesDetail(_ HintsConfig, results map[string]any, ctx ViewCtx) DetailView {
	list, _ := results[DetailHintsSlot].([]DetailHint)
	today := todayOf(ctx)
	var events []Event
	for _, h := range list {
		due, ok := metrics.ParseDay(h.Due)
		if !ok {
			continue
		}
		left := int(due.Sub(today).Hours() / hoursPerDay)
		events = append(events, Event{At: due, Title: h.Title, Sub: h.Why, State: TxtA("detail.days", "n", left), Tier: sevTierName(h.Severity)})
	}
	sort.SliceStable(events, func(a, b int) bool { return events[a].At.Before(events[b].At) })
	body := &DetailBody{}
	if len(events) > 0 {
		body.Line = []Fact{{Label: T("detail.expiry.next"), Value: events[0].Title}, {Label: T("detail.expiry.in"), Value: events[0].State}}
		body.Blocks = []Block{{Kind: BlockTimeline, Label: T("detail.expiry.list"), Hero: true, Data: events}}
	} else {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.expiries.none")}}
	}
	return DetailView{Body: body}
}

// sevTierName is the Kante tier of a severity.
func sevTierName(s enums.Severity) string {
	switch {
	case s >= enums.SeverityCritical:
		return "red"
	case s >= enums.SeverityWarn:
		return "yellow"
	}
	return "cyan"
}

// hintsDetail (record without facts column): the tile's hints with why
// and since.
func hintsDetail(_ HintsConfig, results map[string]any, _ ViewCtx) DetailView {
	list, _ := results[DetailHintsSlot].([]DetailHint)
	crit, warn := 0, 0
	for _, h := range list {
		switch sevTierName(h.Severity) {
		case "red":
			crit++
		case "yellow":
			warn++
		}
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(list), Label: T("detail.hints_open")}, {Value: crit, Label: T("detail.hints_critical"), Tier: tierIf(crit > 0, "red", "")},
		{Value: warn, Label: T("detail.hints_warn"), Tier: tierIf(warn > 0, "yellow", "")}}}
	// Hints with a due date are in the personal iCal feed (/calendar.ics).
	head := DetailHead{Actions: []DetailAction{{LabelKey: "detail.hints_all", Href: "/hints", Primary: true}, {LabelKey: "detail.hints_ics", Href: "/me/notify"}}}
	if len(list) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.hints_none")}}
		return DetailView{Head: head, Body: body}
	}
	// Every hint opens by click: its why, its history, a note or a
	// colleague to take it over.
	objs := &ObjList{Label: T("detail.hints_open"), Sel: -1}
	for i, h := range list {
		id := strconv.FormatInt(h.ID, 10)
		objs.Items = append(objs.Items, LitRow{Name: h.Title, Meta: h.Rule, State: tierState(sevTierName(h.Severity)), Item: id})
		if id == pickedItem(results) {
			objs.Sel = i
		}
	}
	work, picked := results[HintWorkSlot].(HintWork)
	if !picked || objs.Sel < 0 {
		body.Blocks = []Block{{Kind: BlockRows, Label: T("detail.hints_pick"), Data: objs.Items}, {Kind: BlockHints, Data: list}}
		return DetailView{Head: head, Body: body}
	}
	objs.Title, objs.Sub = work.Title, cmp.Or(work.Assignee, "–")
	body.List, body.Facts = objs, nil
	body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: work.Why})
	var steps []Event
	for _, s := range work.History {
		steps = append(steps, Event{At: s.At, Title: Txt("hints.event_" + s.Kind), Sub: s.Note, State: s.Actor, Tier: "cyan"})
	}
	if len(steps) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.hints_history"), Data: steps})
	}
	people := append([]FormOption{{Value: "", Label: Txt("detail.hints_nobody")}}, work.People...)
	body.Blocks = append(body.Blocks, Block{Kind: BlockForm, Label: T("detail.hints_work"), Data: Form{Do: "work", Args: map[string]string{"hint": strconv.FormatInt(work.ID, 10)},
		Submit: T("detail.hints_save"), Fields: []FormField{
			{Name: "assignee", Label: T("detail.hints_assignee"), Kind: FieldSelect, Value: assigneeValue(work.AssigneeID), Options: people},
			{Name: "note", Label: T("detail.hints_note"), Kind: FieldArea}}}})
	return DetailView{Head: head, Body: body}
}

// noiseDetail (list and detail): rules whose hints come and go, the
// noisiest chosen.
func noiseDetail(_ NoiseConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results[NoiseSlot].(NoiseData)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	flaps := append([]Flap(nil), data.Flaps...)
	sort.Slice(flaps, func(a, b int) bool { return flaps[a].Returns > flaps[b].Returns })
	total := 0
	daily := make([]float64, len(data.Daily))
	for i, n := range data.Daily {
		total += n
		daily[i] = float64(n)
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.noise.span"), Value: TxtA("detail.days", "n", len(data.Daily))}, {Label: T("detail.noise.new"), Value: total}}}
	var settings []Block
	if len(flaps) > 0 {
		list := &ObjList{Label: T("detail.noise.rules")}
		ruleIDs := make([]string, len(flaps))
		for i, f := range flaps {
			ruleIDs[i] = f.Rule
			list.Items = append(list.Items, LitRow{Name: Txt("rule_name." + f.Rule), Meta: TxtA("detail.noise.returns", "n", f.Returns), State: tierState(tierIf(f.Returns > noiseLoud, "yellow", "")), Item: f.Rule})
		}
		list.Sel = pickIndex(results, ruleIDs)
		f := flaps[list.Sel]
		list.Title, list.State, list.StateText = Txt("rule_name."+f.Rule), "warn", textArgs("detail.noise.returns", "n", f.Returns)
		list.Sub = f.Rule
		body.List = list
		// The rule's limits as the space has them, with the way to them.
		var rows [][]Cell
		values := rules.NumberSettings(ctx.Settings, f.Rule)
		for _, k := range slices.Sorted(maps.Keys(values)) {
			rows = append(rows, []Cell{{Value: Txt("param." + k)}, {Value: Num(values[k], 2)}})
		}
		if len(rows) > 0 {
			settings = append(settings, Block{Kind: BlockTable, Label: T("detail.noise.limits"), Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: rows, Num: []int{1}}})
		}
		settings = append(settings, Block{Kind: BlockTasks, Data: Tasks{Items: []Task{{Text: Txt("detail.noise.tune"), State: "info",
			Action: T("detail.noise.settings"), Href: "/spaces/settings#rule-" + f.Rule}}}})
	}
	g := ColGraph(daily, "s4")
	g.Ticks = spanTicks(todayOf(ctx), len(daily))
	body.Blocks = []Block{{Kind: BlockGraph, Label: T("detail.noise.per_day"), Data: g}}
	if len(flaps) > 0 {
		body.Blocks = append(body.Blocks, settings...)
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.noise.how")})
	}
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.noise.settings", Href: "/spaces/settings#rules", Primary: true}}}, Body: body}
}

// noiseLoud marks a rule that came back this often.
const noiseLoud = 3

// hintTrendDetail: open hints per level over the days, new per day.
func hintTrendDetail(_ NoiseConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results[NoiseSlot].(NoiseData)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	levels := []struct {
		sev   enums.Severity
		class string
		key   string
	}{{enums.SeverityCritical, "s5", "detail.hints_critical"}, {enums.SeverityWarn, "s2", "detail.hints_warn"}, {enums.SeverityInfo, "s1", "detail.hints_info"}}
	var series []Series
	body := &DetailBody{Side: []Fact{{Label: T("detail.noise.span"), Value: TxtA("detail.days", "n", len(data.Daily))}}}
	for _, l := range levels {
		counts := data.Open[l.sev]
		if len(counts) == 0 {
			continue
		}
		values := make([]float64, len(counts))
		for i, n := range counts {
			values[i] = float64(n)
		}
		series = append(series, Series{Values: values, Class: l.class, Label: ""})
		body.Facts = append(body.Facts, Kpi{Value: counts[len(counts)-1], Label: T(l.key), Tier: map[string]string{"s5": "red", "s2": "yellow", "s1": "cyan"}[l.class]})
	}
	if len(series) > 0 {
		g := LineGraph(series...)
		g.Lo, g.Ticks = 0, spanTicks(todayOf(ctx), len(data.Daily))
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.trend.open"), Hero: true, Data: g})
	}
	daily := make([]float64, len(data.Daily))
	for i, n := range data.Daily {
		daily[i] = float64(n)
	}
	g := ColGraph(daily, "s4")
	g.Ticks = spanTicks(todayOf(ctx), len(daily))
	body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.noise.per_day"), Data: g})
	return DetailView{Body: body}
}

// statusLightDetail (record without facts column): the light, the hints
// that set it.
func statusLightDetail(cfg StatusLightConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := statusLightView(cfg, results, ctx)
	state, _ := view["State"].(string)
	items, _ := view["Items"].([]HintBrief)
	var rows []LitRow
	for _, b := range items {
		rows = append(rows, LitRow{Name: b.Title, Meta: strings.Join(b.Sources, ", "), State: tierState(sevTierName(b.Severity))})
	}
	body := &DetailBody{Facts: []Kpi{{Value: Txt("detail.light." + state), Label: T("detail.light.now"), Tier: state}, {Value: view["Count"], Label: T("detail.light.why_count")}}}
	if len(rows) > 0 {
		body.Blocks = []Block{{Kind: BlockRows, Label: T("detail.light.why"), Data: rows}}
	} else {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.light.calm")}}
	}
	// How the light stood each day: red where any run had a hint at the
	// red level, yellow at the yellow one.
	now := todayOf(ctx)
	days := metrics.LightDays(historyOf(results), now, lightDays, []int{int(enums.SeverityInfo), int(enums.SeverityWarn), int(enums.SeverityCritical)})
	strip := Strip{Name: "", States: make([]string, len(days))}
	red := 0
	for i, top := range days {
		switch {
		case top < 0:
			strip.States[i] = "off"
		case top >= int(cfg.Red):
			strip.States[i], red = "bad", red+1
		case cfg.Yellow > 0 && top >= int(cfg.Yellow):
			strip.States[i] = "warn"
		default:
			strip.States[i] = "ok"
		}
	}
	strip.Value = TxtA("detail.light.red_days", "n", red)
	body.Blocks = append(body.Blocks, Block{Kind: BlockStrips, Label: T("detail.light.history"), Ticks: spanTicks(now, lightDays), Data: []Strip{strip}})
	ks := map[string]string{"green": "ok", "yellow": "warn", "red": "bad"}
	return DetailView{Head: DetailHead{State: ks[state], StateKey: "detail.light." + state}, Body: body}
}

// recentDetail (timeline): updates and hints of the last weeks.
func recentDetail(cfg RecentConfig, results map[string]any, ctx ViewCtx) DetailView {
	all, _ := results[TimelineSlot].([]TimelineItem)
	var events []Event
	updates, opened := 0, 0
	for _, it := range all {
		if cfg.Kinds != "" && (cfg.Kinds == "hints") != isHintKind(it.Kind) {
			continue
		}
		tier := "cyan"
		switch it.Kind {
		case kindUpdate:
			updates++
		case "opened", "reopened":
			tier, opened = "yellow", opened+1
		case "resolved":
			tier = "green"
		}
		title := any(it.Subject)
		if it.Count > 1 {
			title = TxtA("detail.recent.burst", "n", it.Count)
		}
		sub := any(it.Detail)
		if it.Cause != "" {
			sub = TxtA("detail.recent.after_update", "update", it.Cause, "n", it.CauseMin)
		}
		events = append(events, Event{At: it.At, Title: title, Sub: sub, State: Txt("timeline." + it.Kind), Tier: tier})
		if len(events) == recentDetailMax {
			break
		}
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.recent.updates"), Value: updates}, {Label: T("detail.recent.opened"), Value: opened}}}
	if len(events) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("timeline.empty")}}
	} else {
		body.Blocks = []Block{{Kind: BlockTimeline, Label: T("detail.recent.list"), Hero: true, Data: events}}
	}
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.recent.all", Href: "/timeline", Primary: true}}}, Body: body}
}

// todayDetail (timeline): the day as a strip with appointments, timer and
// departures, the list below.
func todayDetail(cfg TodayConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := todayView(cfg, results, ctx)
	items, _ := view["Items"].([]TodayItem)
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	strip := DayStrip{Now: hourOf(now)}
	var rows []LitRow
	for _, it := range items {
		at := it.at.In(loc)
		switch it.Kind {
		case "event":
			strip.Events = append(strip.Events, HourSpan{From: hourOf(at), To: hourOf(at) + 1, Colour: "cyan"})
			rows = append(rows, LitRow{Name: it.Text, Meta: dashIfEmpty(it.At), State: tierIf(it.Past, "off", "info")})
		case "timer":
			strip.Spans = append(strip.Spans, HourSpan{From: hourOf(at), To: hourOf(now), Colour: "d4"})
			rows = append(rows, LitRow{Name: TxtA("today.timer", "what", it.Text), Meta: it.At, State: "ok"})
		case "transit":
			strip.Events = append(strip.Events, HourSpan{From: hourOf(at), To: hourOf(at) + minSpanH, Colour: "warn"})
			rows = append(rows, LitRow{Name: it.Text, Meta: it.At, State: "info"})
		case "deadline":
			rows = append(rows, LitRow{Name: TxtA("deadline."+it.Deadline, "period", it.Period, "year", it.Year), Meta: TxtA("detail.days", "n", it.Left), State: "warn"})
		}
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.today_label"), Value: Day(now)}, {Label: T("detail.now"), Value: now.Format(timeOfDay)}},
		Blocks: []Block{{Kind: BlockDayStrip, Label: T("detail.today_day"), Hero: true, Data: strip}}}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.today_list"), Data: rows})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("today.empty")})
	}
	return DetailView{Body: body}
}

// minSpanH is how wide a moment (a departure) is drawn: a quarter hour.
const minSpanH = .25

func dashIfEmpty(s string) any {
	if s == "" {
		return Txt("today.all_day")
	}
	return s
}

// storyDetail: the week's lines as a report.
func storyDetail(cfg StoryConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := storyView(cfg, results, ctx)
	lines, _ := view["Lines"].([]metrics.StoryLine)
	body := &DetailBody{}
	var rows []LitRow
	for _, l := range lines {
		rows = append(rows, LitRow{Name: TxtA("week."+l.Key, storyArgs(l.Params)...), State: "info"})
	}
	// The weeks before: each opens its story (drawn anew from the history).
	monday := weekStart(todayOf(ctx))
	weeks := []LitRow{{Name: Txt("detail.story.this_week"), Meta: Day(monday), State: "info", Item: "0"}}
	for back := 1; back <= storyWeeksBack; back++ {
		start := monday.AddDate(0, 0, -weekDays*back)
		weeks = append(weeks, LitRow{Name: TxtA("detail.story.week_of", "n", isoWeek(start)), Meta: Day(start), State: "info", Item: strconv.Itoa(back)})
	}
	sel, _ := strconv.Atoi(pickedItem(results))
	body.List = &ObjList{Label: T("detail.story.weeks"), Items: weeks, Sel: min(max(sel, 0), storyWeeksBack), Title: weeks[min(max(sel, 0), storyWeeksBack)].Name}
	if len(rows) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.story.empty")}}
	} else {
		body.Blocks = []Block{{Kind: BlockRows, Label: T("detail.story.week"), Data: rows}}
	}
	return DetailView{Body: body}
}

// storyArgs flattens a story line's parameters into pairs.
func storyArgs(params map[string]any) []any {
	var out []any
	for k, v := range params {
		out = append(out, k, v)
	}
	return out
}

// calEvent is an appointment with its calendar's colour.
type calEvent struct {
	e     sources.Event
	color string
}

// freeGaps are the workdays' free stretches of at least freeMin hours
// between freeFrom and freeTo; appointments count as one hour, all-day
// ones fill the day. "Di 7.  13:00  18:00  5".
func freeGaps(evs []calEvent, today time.Time, zone *time.Location) [][]Cell {
	var rows [][]Cell
	for d := range calendarDetailDays {
		day := today.AddDate(0, 0, d)
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		var busy [][2]float64
		for _, x := range evs {
			at := x.e.Start.In(zone)
			if at.Format(isoDate) != day.Format(isoDate) {
				continue
			}
			if x.e.AllDay {
				busy = append(busy, [2]float64{freeFrom, freeTo})
				continue
			}
			busy = append(busy, [2]float64{hourOf(at), hourOf(at) + 1})
		}
		slices.SortFunc(busy, func(a, b [2]float64) int { return cmp.Compare(a[0], b[0]) })
		from := float64(freeFrom)
		for _, b := range append(busy, [2]float64{freeTo, freeTo}) {
			to := min(b[0], freeTo)
			if to-from >= freeMin {
				rows = append(rows, []Cell{{Value: Day(day)}, {Value: hourClock(day, from)}, {Value: hourClock(day, to)}, {Value: to - from}})
			}
			from = max(from, b[1])
		}
	}
	return rows
}

// hourClock writes an hour of a day: 13.5 → "13:30".
func hourClock(day time.Time, h float64) string {
	return day.Add(time.Duration(h * float64(time.Hour))).Format(timeOfDay)
}

// unbookedRows are the last weeks' appointments of the dialog's look back
// that name a Kimai customer or project without a booking that day.
func unbookedRows(results map[string]any, today time.Time, zone *time.Location) [][]Cell {
	kimai, ok := results[peerKimai].(*sources.KimaiDataset)
	if !ok {
		return nil
	}
	var past sources.CalendarResult
	for i := range calendarSlots {
		name := pastName
		if i > 0 {
			name += strconv.Itoa(i + 1)
		}
		if r, ok := results[name].(*sources.CalendarResult); ok {
			past.Events = append(past.Events, r.Events...)
		}
	}
	var rows [][]Cell
	for _, e := range metrics.UnbookedEvents(&past, kimai, today) {
		rows = append(rows, []Cell{{Value: Day(e.Start.In(zone))}, {Value: e.Start.In(zone).Format(timeOfDay)}, {Value: e.Title}})
	}
	return rows
}

// calendarDetail (timeline with the list): the next two weeks as hour
// lines, then every appointment with its place.
func calendarDetail(cfg CalendarConfig, results map[string]any, ctx ViewCtx) DetailView {
	zone := clockZone()
	today := todayOf(ctx)
	var evs []calEvent
	for i := range calendarSlots {
		name := "events"
		if i > 0 {
			name += fmt.Sprint(i + 1)
		}
		data, ok := results[name].(*sources.CalendarResult)
		if !ok {
			continue
		}
		color := "cyan"
		if i < len(cfg.Colors) && cfg.Colors[i] != "none" {
			color = cfg.Colors[i]
		}
		for _, e := range data.Events {
			evs = append(evs, calEvent{e, color})
		}
	}
	sort.SliceStable(evs, func(a, b int) bool { return evs[a].e.Start.Before(evs[b].e.Start) })
	week := Week{Start: weekLineStart, Span: weekLineSpan}
	now := time.Now().In(zone)
	for d := range calendarDetailDays {
		day := today.AddDate(0, 0, d)
		row := WeekDay{Label: TxtA("detail.calendar.day", "wd", Txt(weekdayKeys[(int(day.Weekday())+6)%7]), "d", day.Day()), Today: d == 0, Now: -1}
		n := 0
		for _, x := range evs {
			at := x.e.Start.In(zone)
			if at.Format(isoDate) != day.Format(isoDate) {
				continue
			}
			n++
			from, to := hourOf(at), hourOf(at)+1
			if x.e.AllDay {
				from, to = float64(weekLineStart), float64(weekLineStart+weekLineSpan)
			}
			row.Spans = append(row.Spans, HourSpan{From: from, To: to, Colour: x.color})
		}
		row.Sum = n
		if row.Today {
			row.Now = hourOf(now)
		}
		week.Days = append(week.Days, row)
	}
	var rows [][]Cell
	for _, x := range evs {
		when := any(x.e.Start.In(zone).Format(timeOfDay))
		if x.e.AllDay {
			when = Txt("today.all_day")
		}
		rows = append(rows, []Cell{{Value: Day(x.e.Start.In(zone))}, {Value: when}, {Value: x.e.Title}, {Value: x.e.Location}})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.calendar.count"), Value: len(evs)}},
		Blocks: []Block{{Kind: BlockWeek, Label: T("detail.calendar.weeks"), Hero: true, Data: week}}}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.calendar.list"), Data: Table{Head: []Text{T("detail.calendar.date"),
			T("detail.calendar.time"), T("detail.calendar.title"), T("detail.calendar.place")}, Rows: rows}})
	}
	if gaps := freeGaps(evs, today, zone); len(gaps) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.calendar.free"), Data: Table{Head: []Text{T("detail.calendar.date"),
			T("detail.calendar.from"), T("detail.calendar.to"), T("detail.calendar.hours")}, Rows: gaps}})
	}
	if rows := unbookedRows(results, today, zone); len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.calendar.unbooked"), Meta: TxtA("detail.calendar.unbooked_note", "n", metrics.UnbookedDays),
			Data: Table{Head: []Text{T("detail.calendar.date"), T("detail.calendar.time"), T("detail.calendar.title")}, Rows: rows}})
	}
	return DetailView{Body: body}
}

// clockDetail: every zone with its offset and whether it is working time.
func clockDetail(cfg ClockConfig, _ map[string]any, _ ViewCtx) DetailView {
	here := time.Now()
	week := Week{Start: 0, Span: hoursPerDay}
	var cards []Card
	for _, name := range cfg.Timezones {
		loc, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		t := here.In(loc)
		_, off := t.Zone()
		_, hereOff := here.Zone()
		diff := float64(off-hereOff) / secondsPerHour
		cards = append(cards, Card{Label: Plain(name), Value: t.Format(timeOfDay), Sub: fmt.Sprintf("%+.1f h", diff)})
		// Working hours 9–18 there, drawn in the viewer's hours.
		from, to := workFrom-diff, workTo-diff
		week.Days = append(week.Days, WeekDay{Label: Plain(name).Args["text"], Spans: clampSpans(from, to), Sum: t.Format(timeOfDay), Now: hourOf(here)})
	}
	return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockWall, Data: cards}, {Kind: BlockWeek, Label: T("detail.clock.work"), Data: week}}}}
}

const (
	secondsPerHour = 3600
	workFrom       = 9
	workTo         = 18
)

// clampSpans keeps a span inside the day, split where it crosses midnight.
func clampSpans(from, to float64) []HourSpan {
	var out []HourSpan
	for _, shift := range []float64{-hoursPerDay, 0, hoursPerDay} {
		a, b := max(from+shift, 0), min(to+shift, hoursPerDay)
		if b > a {
			out = append(out, HourSpan{From: a, To: b, Colour: "aqua"})
		}
	}
	return out
}

// assigneeValue is the select value of a hint's assignee, "" for nobody.
func assigneeValue(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// deadlineTickDays is how far ahead deadlines can be ticked as filed.
const deadlineTickDays = 45

// lightDays is the span of the status light's history.
const lightDays = 30

// storyWeeksBack is how many past weeks the story dialog lists (as
// widgetlib's storyArchive loads them).
const storyWeeksBack = 8
