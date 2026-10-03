package widgets

// Detail dialogs of the overview tiles: links, connections, deadlines,
// hints, timeline, today, week story, calendar, clock.

import (
	"cmp"
	"fmt"
	"sort"
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
		rows = append(rows, []Cell{{Value: l.Title}, {Value: l.URL}, {Value: since, State: "bad"}})
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(links), Label: T("detail.links.down"), Tier: tierIf(len(links) > 0, "red", "green")}}}
	if len(links) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.links.all_up")}}
		return DetailView{Body: body}
	}
	body.Facts = append(body.Facts, Kpi{Value: TxtA("detail.days", "n", longest), Label: T("detail.links.longest")})
	body.Blocks = []Block{{Kind: BlockTable, Label: T("detail.links.list"), Data: Table{Head: []Text{T("detail.links.title"), T("detail.links.url"), T("detail.links.since_label")}, Rows: rows}}}
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
	body.Blocks = []Block{{Kind: BlockStrips, Label: T("detail.conn.days"), Ticks: spanTicks(todayOf(ctx), ConnHealthDays), Data: rows}}
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
	if len(list) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.hints_none")}}
	} else {
		body.Blocks = []Block{{Kind: BlockHints, Data: list}}
	}
	head := DetailHead{Actions: []DetailAction{{LabelKey: "detail.hints_all", Href: "/hints", Primary: true}}}
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
	if len(flaps) > 0 {
		list := &ObjList{Label: T("detail.noise.rules")}
		for _, f := range flaps {
			list.Items = append(list.Items, LitRow{Name: f.Rule, Meta: TxtA("detail.noise.returns", "n", f.Returns), State: tierState(tierIf(f.Returns > noiseLoud, "yellow", ""))})
		}
		list.Title, list.State, list.StateText = flaps[0].Rule, "warn", textArgs("detail.noise.returns", "n", flaps[0].Returns)
		list.Sub = Txt("detail.noise.tune")
		body.List = list
	}
	g := ColGraph(daily, "s4")
	g.Ticks = spanTicks(todayOf(ctx), len(daily))
	body.Blocks = []Block{{Kind: BlockGraph, Label: T("detail.noise.per_day"), Data: g}}
	if len(flaps) > 0 {
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
		events = append(events, Event{At: it.At, Title: title, Sub: it.Detail, State: Txt("timeline." + it.Kind), Tier: tier})
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

// calendarDetail (timeline with the list): the next two weeks as hour
// lines, then every appointment with its place.
func calendarDetail(cfg CalendarConfig, results map[string]any, ctx ViewCtx) DetailView {
	zone := clockZone()
	today := todayOf(ctx)
	type ev struct {
		e     sources.Event
		color string
	}
	var evs []ev
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
			evs = append(evs, ev{e, color})
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
