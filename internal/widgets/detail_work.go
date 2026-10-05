package widgets

// Detail dialogs of the work tiles: Kimai, Kintsugi, Dawarich, month close.

import (
	"cmp"
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	weekLineStart = 6  // the week lines show 06–22 h
	weekLineSpan  = 16 // hours
	heatDetailWks = 53
	weeksShown    = 6
)

// hourOf is a time of day in hours: 08:15 → 8.25.
func hourOf(t time.Time) float64 {
	return float64(t.Hour()) + float64(t.Minute())/minutesPerHour
}

// spanColour is a Kimai entry's colour: its own, else a data colour per key.
func spanColour(own string, key int64) string {
	if strings.HasPrefix(own, "#") && len(own) == len("#rrggbb") {
		return own
	}
	return "d" + fmt.Sprint(int(key%6)+1)
}

// timerDetail (timeline): today as a strip of booked spans, the running
// timer, the recent combinations to switch to.
func timerDetail(_ KimaiLiteConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["live"].(*sources.KimaiLive)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	now := time.Now()
	strip := DayStrip{Now: hourOf(now)}
	for _, s := range data.Today {
		strip.Spans = append(strip.Spans, HourSpan{From: hourOf(s.Begin.In(now.Location())), To: hourOf(s.End.In(now.Location())), Colour: "d4"})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.timer.today"), Value: clockMinutes(data.TodayMin)}, {Label: T("detail.timer.week"), Value: clockMinutes(data.WeekMin)}}}
	head := DetailHead{State: "off", StateKey: "detail.timer.idle", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if week := data.Contract.WeekMinutes(); week > 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.timer.target"), Value: clockMinutes(week)})
	}
	if len(data.Active) > 0 {
		t := data.Active[0]
		strip.Spans = append(strip.Spans, HourSpan{From: hourOf(t.Begin.In(now.Location())), To: hourOf(now), Colour: spanColour(t.Color, t.ProjectID)})
		head.State, head.StateKey, head.StateArgs = "ok", "detail.timer.running", map[string]any{"time": clockSeconds(now.Sub(t.Begin))}
		body.Facts = []Kpi{{Value: clockSeconds(now.Sub(t.Begin)), Label: T("detail.timer.running_for"), Tier: "cyan"}, {Value: t.Project, Label: T("detail.timer.project")},
			{Value: t.Activity, Label: T("detail.timer.activity")}}
		if t.Description != "" {
			body.Line = append(body.Line, Fact{Label: T("detail.timer.note"), Value: t.Description})
		}
		// What is left of the project's budget, so booking shows when it
		// gets tight.
		if kimai, ok := results[openName].(*sources.KimaiDataset); ok {
			for _, p := range kimai.Projects {
				if share, has := metrics.BudgetUse(p, kimai, todayOf(ctx)); p.ID == t.ProjectID && has {
					limits := budgetLimitsOf(ctx)
					body.Facts = append(body.Facts, Kpi{Value: NumU(share*percentScale, 0, "%"), Label: T("detail.timer.budget_used"),
						Tier: tierIf(share >= limits.critical, "red", tierIf(share >= limits.warn, "yellow", "green"))})
				}
			}
		}
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockDayStrip, Label: T("detail.timer.day"), Hero: true, Data: strip})
	var recent []LitRow
	for _, t := range data.Recent {
		recent = append(recent, LitRow{Name: t.Project + " · " + t.Activity, Meta: t.Customer, State: "info"})
	}
	if len(recent) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.timer.recent"), Data: recent})
	}
	return DetailView{Head: head, Body: body}
}

// weekLines draws a week of Kimai entries as Kante week lines.
func weekLines(data *sources.KimaiDataset, today time.Time, kind metrics.Hours) Week {
	monday := weekStart(today)
	w := Week{Start: weekLineStart, Span: weekLineSpan}
	sums := make([]int, weekDays)
	spans := make([][]HourSpan, weekDays)
	colours := kimaiColours(data)
	for _, s := range data.Timesheets {
		if kind == metrics.HoursBillable && !s.Billable {
			continue
		}
		begin, err := time.Parse(time.RFC3339, s.Begin)
		if err != nil {
			continue
		}
		i := int(metrics.Today(begin).Sub(monday).Hours() / hoursPerDay)
		if i < 0 || i >= weekDays {
			continue
		}
		sums[i] += s.Minutes
		from := hourOf(begin)
		spans[i] = append(spans[i], HourSpan{From: from, To: from + float64(s.Minutes)/minutesPerHour, Colour: spanColour(colours[s.ProjectID], s.ProjectID)})
	}
	for i := range spans {
		spans[i] = append(spans[i], dayGaps(spans[i])...)
	}
	todayIdx := int(today.Sub(monday).Hours() / hoursPerDay)
	now := hourOf(time.Now())
	for i := range weekDays {
		day := WeekDay{Label: Txt(weekdayKeys[i]), Spans: spans[i], Sum: clockMinutes(sums[i]), Today: i == todayIdx, Now: -1}
		if day.Today {
			day.Now = now
		}
		w.Days = append(w.Days, day)
	}
	return w
}

// kimaiColours are the projects' colours as Kimai shows them: the
// project's own, else its customer's.
func kimaiColours(data *sources.KimaiDataset) map[int64]string {
	byCustomer := map[int64]string{}
	for _, c := range data.Customers {
		byCustomer[c.ID] = c.Color
	}
	out := map[int64]string{}
	for _, p := range data.Projects {
		out[p.ID] = cmp.Or(p.Color, byCustomer[p.CustomerID])
	}
	return out
}

// dayGaps are the stretches between a day's first and last entry that
// nothing covers: a forgotten booking or a long break.
func dayGaps(spans []HourSpan) []HourSpan {
	if len(spans) < minPoints {
		return nil
	}
	sorted := append([]HourSpan(nil), spans...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].From < sorted[b].From })
	var gaps []HourSpan
	end := sorted[0].To
	for _, s := range sorted[1:] {
		if s.From-end >= gapMinHours {
			gaps = append(gaps, HourSpan{From: end, To: s.From, Colour: gapColour})
		}
		end = max(end, s.To)
	}
	return gaps
}

// A gap counts from a quarter hour; it is drawn in the divider colour.
const (
	gapMinHours = 0.25
	gapColour   = "bg2"
)

// weekdayKeys name the days of a week line, Monday first.
var weekdayKeys = []string{"detail.wd.mon", "detail.wd.tue", "detail.wd.wed", "detail.wd.thu", "detail.wd.fri", "detail.wd.sat", "detail.wd.sun"}

// kimaiWeekDetail (timeline): the week as hour lines, the last weeks.
func kimaiWeekDetail(cfg KimaiWeekConfig, data *sources.KimaiDataset, ctx ViewCtx, _ map[string]any) DetailView {
	today := todayOf(ctx)
	view := kimaiWeekView(cfg, data, ctx)
	body := &DetailBody{Line: []Fact{{Label: T("detail.kimai.booked"), Value: view["Total"]}}}
	if week := data.Contract.WeekMinutes(); week > 0 {
		key := "detail.kimai.left"
		if view["Over"] == true {
			key = "detail.kimai.over"
		}
		body.Line = append(body.Line, Fact{Label: T("detail.kimai.target"), Value: clockMinutes(week)}, Fact{Label: T(key), Value: view["Left"]})
	}
	kind := metrics.HoursAll
	if cfg.BillableOnly {
		kind = metrics.HoursBillable
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockWeek, Label: T("detail.kimai.this_week"), Hero: true, Data: weekLines(data, today, kind)})

	// The last weeks' sums against the contract.
	var sums []float64
	var ticks []any
	for back := weeksShown - 1; back >= 0; back-- {
		total, _ := weekMinutes(data, today, weekPick{Back: back, Billable: cfg.BillableOnly})
		m := 0
		for _, v := range total {
			m += v
		}
		sums = append(sums, float64(m)/minutesPerHour)
		ticks = append(ticks, TxtA("detail.kimai.kw", "n", isoWeek(weekStart(today).AddDate(0, 0, -weekDays*back))))
	}
	g := ColGraph(sums, "s4")
	g.Ticks = ticks
	if week := data.Contract.WeekMinutes(); week > 0 {
		g.Goal, g.HasGoal = float64(week)/minutesPerHour, true
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.kimai.last_weeks"), Data: g})
	return DetailView{Body: body}
}

func isoWeek(t time.Time) int {
	_, w := t.ISOWeek()
	return w
}

// kimaiSplitDetail (wall): one card per customer (or project) with the
// last weeks.
func kimaiSplitDetail(cfg KimaiSplitConfig, data *sources.KimaiDataset, ctx ViewCtx, _ map[string]any) DetailView {
	today := todayOf(ctx)
	names := metrics.KimaiCustomerNames(data)
	colours := map[int64]string{}
	for _, c := range data.Customers {
		colours[c.ID] = c.Color
	}
	if cfg.ByProject {
		colours = kimaiColours(data)
		names = map[int64]string{}
		for _, p := range data.Projects {
			names[p.ID] = p.Name
		}
	}
	back := 0
	if cfg.LastWeek {
		back = 1
	}
	perWeek := make([]map[int64]int, weeksShown)
	for i := range weeksShown {
		_, byKey := weekMinutes(data, today, weekPick{Back: back + weeksShown - 1 - i, ByProject: cfg.ByProject})
		perWeek[i] = map[int64]int{}
		for _, day := range byKey {
			for k, m := range day {
				perWeek[i][k] += m
			}
		}
	}
	current := perWeek[weeksShown-1]
	total := 0
	keys := make([]int64, 0, len(current))
	for k, m := range current {
		keys = append(keys, k)
		total += m
	}
	sort.Slice(keys, func(a, b int) bool { return current[keys[a]] > current[keys[b]] })
	var cards []Card
	for _, k := range keys {
		var spark []float64
		for _, wk := range perWeek {
			spark = append(spark, float64(wk[k])/minutesPerHour)
		}
		name := names[k]
		if name == "" {
			name = "?"
		}
		cards = append(cards, Card{Label: Plain(name), Value: clockMinutes(current[k]), Spark: spark, Sub: NumU(float64(current[k])*percentScale/float64(max(total, 1)), 0, "%"),
			Colour: colours[k]})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.kimai.booked"), Value: clockMinutes(total)}}}
	if len(cards) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.kimai.none")}}
	} else {
		body.Blocks = []Block{{Kind: BlockWall, Data: cards}}
	}
	return DetailView{Body: body}
}

// heatmapDetail (record without the facts column): a year of days, the
// longest and the mean day.
func heatmapDetail(cfg HeatConfig, data *sources.KimaiDataset, ctx ViewCtx, _ map[string]any) DetailView {
	perDay := metrics.HoursByDay(data)
	today := todayOf(ctx)
	lastMonday := weekStart(today)
	first := lastMonday.AddDate(0, 0, -weekDays*(heatDetailWks-1))
	heat := Heat{Rows: weekDays}
	worked, sum, long := 0, 0, 0
	longest, longestDay := 0, ""
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		m := perDay[d.Format(isoDate)]
		heat.Levels = append(heat.Levels, heatLevel(m))
		if m > 0 {
			worked, sum = worked+1, sum+m
		}
		if m > longDayMin {
			long++
		}
		if m > longest {
			longest, longestDay = m, d.Format(isoDate)
		}
	}
	heat.Ticks = []any{Day(first), Txt("detail.today")}
	body := &DetailBody{Line: []Fact{{Label: T("detail.heat.span"), Value: TxtA("detail.heat.weeks", "n", heatDetailWks)}}}
	body.Facts = []Kpi{{Value: NumU(float64(sum)/minutesPerHour, 0, "h"), Label: T("detail.heat.total")}, {Value: worked, Label: T("detail.heat.days")},
		{Value: long, Label: T("detail.heat.long"), Tier: tierIf(long > 0, "yellow", "")}}
	if worked > 0 {
		body.Facts = append(body.Facts, Kpi{Value: clockMinutes(sum / worked), Label: T("detail.heat.mean")})
		body.Line = append(body.Line, Fact{Label: T("detail.heat.longest"), Value: TxtA("detail.heat.at_day", "time", clockMinutes(longest), "day", DayS(longestDay))})
	}
	body.Blocks = []Block{{Kind: BlockHeat, Label: T("detail.heat.per_day"), Hero: true, Data: heat}}

	// When in the week the hours fall: evenings and Fridays that run long.
	pattern := metrics.KimaiHourPattern(data, first, today.AddDate(0, 0, 1), clockZone())
	most := 0.0
	for _, day := range pattern {
		for _, m := range day {
			most = max(most, m)
		}
	}
	if most > 0 {
		hours := Heat{Rows: weekDays, Ticks: []any{"00:00", "12:00", "23:00"}}
		for h := range hoursPerDay {
			for wd := range weekDays {
				level := 0
				if m := pattern[wd][h]; m > 0 {
					level = min(int(m/most*heatSteps)+1, heatSteps)
				}
				hours.Levels = append(hours.Levels, level)
			}
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockHeat, Label: T("detail.heat.by_hour"), Meta: Txt("detail.heat.by_hour_scale"), Data: hours})
	}
	return DetailView{Body: body}
}

// longDayMin marks a long working day: over ten hours.
const longDayMin = 10 * minutesPerHour

// unbilledAgeDetail (record without the facts column): the bands and the
// customers.
func unbilledAgeDetail(cfg AgingConfig, data *sources.KimaiDataset, ctx ViewCtx, results map[string]any) DetailView {
	view := unbilledAgeView(cfg, data, ctx)
	bands, _ := view["Bands"].([]AgingBand)
	rows, _ := view["Rows"].([]UnbilledRow)
	total, _ := view["Total"].(float64)
	currency := "EUR"
	var bars []ShareBar
	for _, b := range bands {
		bars = append(bars, ShareBar{Name: Txt("detail.aging." + b.Key), Pct: float64(b.Pct), Value: Money(b.Amount, currency), Tier: b.Tier})
	}
	var table [][]Cell
	for _, r := range rows {
		table = append(table, []Cell{{Value: r.Customer}, {Value: Money(r.Fresh, currency)}, {Value: Money(r.Mid, currency), State: stateIf(r.Mid > 0, "warn")},
			{Value: Money(r.Old, currency), State: stateIf(r.Old > 0, "bad")}, {Value: Money(r.Total, currency)}})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.aging.bands"), Value: TxtA("detail.aging.days", "mid", cfg.Mid, "old", cfg.Old)}},
		Facts:  []Kpi{{Value: Money(total, currency), Label: T("detail.aging.total")}},
		Blocks: []Block{{Kind: BlockBars, Label: T("detail.aging.by_age"), Data: bars}}}
	if len(bands) == 3 && bands[2].Amount > 0 {
		body.Facts = append(body.Facts, Kpi{Value: Money(bands[2].Amount, currency), Label: textArgs("detail.aging.over", "n", cfg.Old), Tier: "red"})
	}
	if len(table) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.aging.by_customer"), Data: Table{Head: []Text{T("detail.aging.customer"),
			T("detail.aging.fresh"), T("detail.aging.mid"), T("detail.aging.old"), T("detail.aging.sum")}, Rows: table, Num: []int{1, 2, 3, 4}}})
	}
	now := todayOf(ctx)
	if open := dailySeries(historyOf(results), metrics.SampleKey("kimai", "unbilled"), now, historyDetailDays); hasValues(open) {
		g := LineGraph(Series{Values: open, Class: "s4"})
		g.Lo, g.Ticks = 0, spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.aging.history"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// closeLateDays: a month closed later than this after its end is late.
const closeLateDays = 10

// textArgs is a label Text with parameters given as pairs.
func textArgs(key string, kv ...any) Text {
	args := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			args[k] = kv[i+1]
		}
	}
	return Text{Key: key, Args: args}
}

// kintsugiDetail: suggestions by status, the open ones, the last run and
// the research budget.
func kintsugiDetail(cfg PickConfig, data *sources.KintsugiDataset, ctx ViewCtx, results map[string]any) DetailView {
	body := &DetailBody{}
	if data.LastRun != nil {
		body.Side = append(body.Side, Fact{Label: T("detail.kintsugi.last_run"), Value: agoOf(data.LastRun.At)},
			Fact{Label: T("detail.state"), Value: data.LastRun.Status, State: stateIf(data.LastRun.Status == sources.KintsugiRunFailed, "bad")})
	}
	body.Side = append(body.Side, Fact{Label: T("detail.kintsugi.gaps"), Value: data.GapsOpen})
	if data.Research {
		body.Side = append(body.Side, Fact{Label: T("detail.kintsugi.budget"), Value: fmt.Sprintf("%.2f / %.2f $", data.UsedUSD, data.BudgetUSD)})
	}
	body.Facts = []Kpi{{Value: len(data.Open), Label: T("detail.kintsugi.open")}}
	if data.Rate >= 0 {
		body.Facts = append(body.Facts, Kpi{Value: NumU(float64(data.Rate), 0, "%"), Label: T("detail.kintsugi.rate")})
	}
	most := max(1, data.New, data.Accepted, data.Snoozed, data.Done, data.Rejected)
	bar := func(key string, n int, tier string) ShareBar {
		return ShareBar{Name: Txt("detail.kintsugi." + key), Pct: float64(n) * percentScale / float64(most), Value: n, Tier: tier}
	}
	var open [][]Cell
	for _, s := range data.Open {
		if cfg.Only == "" || string(s.Kind) == cfg.Only {
			open = append(open, []Cell{{Value: Txt("detail.kintsugi.kind_" + string(s.Kind))}, {Value: s.Title}, {Value: agoOf(s.Created)}})
		}
	}
	body.Blocks = append(body.Blocks, pairOf([]Block{
		{Kind: BlockBars, Label: T("detail.kintsugi.by_status"), Data: []ShareBar{bar("new", data.New, ""), bar("accepted", data.Accepted, "green"),
			bar("snoozed", data.Snoozed, ""), bar("done", data.Done, "green"), bar("rejected", data.Rejected, "")}},
		{Kind: BlockTable, Label: T("detail.kintsugi.open"), Data: Table{Head: []Text{T("detail.kintsugi.kind"), T("detail.kintsugi.title"), T("detail.kintsugi.since")}, Rows: open}},
	})...)
	now := todayOf(ctx)
	if rate := dailySeries(historyOf(results), metrics.SampleKey("kintsugi", "rate"), now, historyDetailDays); hasValues(rate) {
		g := LineGraph(Series{Values: rate, Class: "s2"})
		g.Lo, g.Hi, g.Ticks = 0, percentScale, spanTicks(now, historyDetailDays)
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.kintsugi.rate_history"), Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.kintsugi.run_ok", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if data.LastRun != nil && data.LastRun.Status == sources.KintsugiRunFailed {
		head.State, head.StateKey = "bad", "detail.kintsugi.run_failed"
	}
	return DetailView{Head: head, Body: body}
}

// dawarichDetail (timeline): the day's places as a strip, the visits.
func dawarichDetail(cfg DawarichConfig, data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) DetailView {
	now := time.Now()
	day := ctx.Today
	strip := DayStrip{Now: hourOf(now)}
	if cfg.Yesterday {
		day, strip.Now = todayOf(ctx).AddDate(0, 0, -1).Format(time.DateOnly), -1
	}
	var visits [][]Cell
	minutes, n := 0, 0
	for i, v := range data.Visits {
		begin := dawarichTime(v.Start)
		if begin.IsZero() || begin.In(now.Location()).Format(time.DateOnly) != day {
			continue
		}
		end := dawarichTime(v.End)
		to := now
		if !end.IsZero() {
			to = end
		}
		strip.Spans = append(strip.Spans, HourSpan{From: hourOf(begin.In(now.Location())), To: hourOf(to.In(now.Location())), Colour: dataClassToken(i)})
		endText := any("…")
		if !end.IsZero() {
			endText = end.In(now.Location()).Format("15:04")
		}
		visits = append(visits, []Cell{{Value: v.Name}, {Value: begin.In(now.Location()).Format("15:04")}, {Value: endText}, {Value: clockMinutes(v.Minutes)}})
		minutes, n = minutes+v.Minutes, n+1
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.dawarich.day"), Value: DayS(day)}, {Label: T("detail.dawarich.places"), Value: n},
		{Label: T("detail.dawarich.last_point"), Value: agoOf(dawarichTime(data.LastPoint))}},
		Blocks: []Block{{Kind: BlockDayStrip, Label: T("detail.dawarich.strip"), Hero: true, Data: strip}}}
	if m, ok := dawarichMap(data, results, day, now.Location()); ok {
		body.Blocks = append(body.Blocks, Block{Kind: BlockMap, Label: T("detail.dawarich.route"), Data: m})
	}
	if len(visits) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.dawarich.visits"), Data: Table{Head: []Text{T("detail.dawarich.place"),
			T("detail.dawarich.from"), T("detail.dawarich.to"), T("detail.dawarich.time")}, Rows: visits}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.dawarich.none")})
	}
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}, Body: body}
}

// dawarichMap: the day's track (read on open) with the day's visits as
// points.
func dawarichMap(data *sources.DawarichDataset, results map[string]any, day string, zone *time.Location) (*MapData, bool) {
	var route []GeoPoint
	if r, ok := results[openName].(*sources.DawarichRoute); ok {
		for _, p := range r.Points {
			route = append(route, GeoPoint{p.Lat, p.Lon})
		}
	}
	var marks []MapMark
	for _, v := range data.Visits {
		begin := dawarichTime(v.Start)
		if v.Lat == nil || v.Lon == nil || begin.In(zone).Format(time.DateOnly) != day {
			continue
		}
		marks = append(marks, Pin(GeoPoint{*v.Lat, *v.Lon}, v.Name, "ok"))
	}
	return NewMap(route, marks)
}

// dataClassToken is the colour token of the i-th data colour: d1 … d6.
func dataClassToken(i int) string { return "d" + dataClass(i)[1:] }

// travelDetail (list and detail): the month's trips, the chosen one in
// detail, the month and year.
func travelDetail(cfg TravelConfig, data *sources.DawarichDataset, ctx ViewCtx, results map[string]any) DetailView {
	if cfg.KMRate == 0 {
		cfg.KMRate = defaultKMRate
	}
	today := todayOf(ctx)
	view := travelView(cfg, data, ctx, results)
	trips := metrics.Trips(data, travelAreas(data, ctx, results), metrics.MonthStart(today), today)
	currency := "EUR"
	list := &ObjList{Label: T("detail.travel.trips")}
	days := make([]string, len(trips))
	for i, t := range trips {
		days[i] = t.Day
		list.Items = append(list.Items, LitRow{Name: DayS(t.Day), Meta: NumU(t.KM, 0, "km"), State: "info", Item: t.Day})
	}
	body := &DetailBody{Facts: []Kpi{{Value: NumU(asF(view["MonthKM"]), 0, "km"), Label: T("detail.travel.month")},
		{Value: Money(asF(view["TripKM"])*cfg.KMRate, currency), Label: T("detail.travel.money"), Tier: "cyan"},
		{Value: NumU(asF(view["YearKM"]), 0, "km"), Label: T("detail.travel.year")}}}
	if len(trips) > 0 {
		list.Sel = pickIndex(results, days)
		t := trips[list.Sel]
		list.Title, list.Sub = DayS(t.Day), t.Area
		body.List = list
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Data: Table{Head: []Text{T("detail.exposure.what"), T("detail.exposure.value")}, Rows: [][]Cell{
			{{Value: Txt("detail.travel.km")}, {Value: NumU(t.KM, 0, "km")}}, {{Value: Txt("detail.travel.away")}, {Value: clockMinutes(t.AwayMin)}},
			{{Value: Txt("detail.travel.amount")}, {Value: Money(t.KM*cfg.KMRate, currency)}}}}})
	} else {
		body.Blocks = append(body.Blocks, Block{Kind: BlockText, Data: Txt("detail.travel.none")})
	}
	month, prev := asF(view["MonthKM"]), asF(view["PrevKM"])
	top := max(month, prev)
	body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("detail.travel.compare"), Data: []ShareBar{
		{Name: Txt("detail.travel.this_month"), Pct: pctOfF(month, top), Value: NumU(month, 0, "km")},
		{Name: Txt("detail.travel.last_month"), Pct: pctOfF(prev, top), Value: NumU(prev, 0, "km")}}})

	// The year by month against the year before, from Dawarich's stats.
	thisYear, lastYear := make([]float64, monthsPerYear), make([]float64, monthsPerYear)
	for m := range monthsPerYear {
		thisYear[m] = metrics.DawarichMonthKM(data.Stats, time.Date(today.Year(), time.Month(m+1), 1, 0, 0, 0, 0, time.UTC))
		lastYear[m] = metrics.DawarichMonthKM(data.Stats, time.Date(today.Year()-1, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC))
	}
	if hasValues(thisYear) {
		g := Graph{Kind: GraphCols, Mark: int(today.Month()) - 1, Series: []Series{{Values: thisYear, Class: "s1", Label: fmt.Sprint(today.Year())},
			{Values: lastYear, Class: "s3", Label: fmt.Sprint(today.Year() - 1)}}, Ticks: []any{"01", "12"}}
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.travel.by_month"), Meta: "km", Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// pctOfF is part as percent of whole, at most 100.
func pctOfF(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return min(part/whole*percentScale, percentScale)
}

// monthCloseDetail: the month's steps as tasks with their links.
func monthCloseDetail(cfg MonthCloseConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := monthCloseView(cfg, results, ctx)
	steps, _ := view["Steps"].([]CloseStep)
	tasks := Tasks{Label: T("detail.month_close.steps")}
	for _, s := range steps {
		tasks.Total++
		t := Task{State: "warn", Action: T("detail.open_in"), Href: s.URL}
		switch s.Key {
		case "hours":
			t.Text = TxtA("close.hours", "hours", Num(s.Hours, 1))
		case "receipts":
			t.Text = TxtA("close.receipts", "n", s.Count)
			if s.Count == 0 && !s.Done {
				t.Text = Txt("close.receipts_setup")
			}
		case "drafts", "inbox":
			t.Text = TxtA("close."+s.Key, "n", s.Count)
		case "vat":
			t.Text = TxtA("close.vat", "day", DayS(s.Due))
		}
		if s.Done {
			tasks.Done++
			t.State, t.Href = "ok", ""
			if s.Key != "vat" {
				t.Text = Txt("close." + s.Key + "_done")
			}
			if s.Hand {
				t.Meta = Txt("detail.month_close.by_hand")
			}
		}
		tasks.Items = append(tasks.Items, t)
	}
	if len(steps) == 0 {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("close.none")}}}}
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockTasks, Data: tasks}}}
	// When each month was seen closed, days after its end.
	var closed [][]Cell
	if h := historyOf(results); h != nil {
		for _, e := range h.Events {
			month, err := time.Parse("2006-01", e.Subject)
			if e.Kind != metrics.EventClose || err != nil {
				continue
			}
			after := int(e.At.Sub(metrics.AddMonths(month, 1)).Hours() / hoursPerDay)
			closed = append(closed, []Cell{{Value: month.Format("01/2006")}, {Value: Day(e.At)}, {Value: TxtA("detail.days", "n", after), State: stateIf(after > closeLateDays, "warn")}})
		}
	}
	if len(closed) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.month_close.history"),
			Data: Table{Head: []Text{T("detail.month_close.month"), T("detail.month_close.closed"), T("detail.month_close.after")}, Rows: closed, Num: []int{2}}})
	}
	return DetailView{Body: body}
}
