package widgets

// Detail dialogs of the home and world tiles: weather warnings, energy,
// Grocy, Home Assistant, weather, markets, boards and holidays.

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	holidayDetailWks = 26  // weeks of the holiday raster
	pricesQuarter    = 4   // the dearest quarter of the hours is marked
	centsPerUnit     = 100 // prices come per kWh in the currency, shown in cents
	percentBase      = 100 // a market line starts at 100
	holidayLevel     = 4   // heat level of a holiday
	absentLevel      = 3   // heat level of a day off in Kimai
	bridgeLevel      = 2   // heat level of a bridge day
)

// dwdTiers maps DWD severities to tiers of the timeline.
var dwdTiers = map[string]string{sources.WarnMinor: "cyan", sources.WarnModerate: "yellow", "severe": "red", "extreme": "red"}

// dwdDetail (record without the facts column): the warnings over time.
func dwdDetail(cfg DWDConfig, data *sources.DWDDataset, _ ViewCtx, results map[string]any) DetailView {
	shown, _ := dwdView(cfg, data, ViewCtx{})["Data"].(*sources.DWDDataset)
	zone := clockZone()
	var events []Event
	worst, until := 0, time.Time{}
	for _, w := range shown.Warnings {
		sev := strings.ToLower(w.Severity)
		worst = max(worst, metrics.WarningRank(sev))
		if w.Expire.After(until) {
			until = w.Expire
		}
		events = append(events, Event{At: w.Onset.In(zone), Title: w.Headline,
			Sub:   TxtA("detail.dwd.span", "from", w.Onset.In(zone).Format(timeOfDay), "day", Day(w.Expire.In(zone)), "to", w.Expire.In(zone).Format(timeOfDay)),
			State: Txt("dwd." + sev), Tier: dwdTiers[sev]})
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.dwd.place"), Value: shown.Place}, {Label: T("detail.dwd.count"), Value: len(events)}}}
	head := DetailHead{State: "ok", StateKey: "detail.dwd.calm", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if len(events) == 0 {
		body.Blocks = append([]Block{{Kind: BlockText, Data: Txt("detail.dwd.none")}}, hintsBlock(results)...)
		return DetailView{Head: head, Body: body}
	}
	head.State, head.StateKey = stateIf(worst > 1, "warn"), "detail.dwd.active"
	if head.State == "" {
		head.State = "info"
	}
	body.Facts = []Kpi{{Value: len(events), Label: T("detail.dwd.count"), Tier: "yellow"}, {Value: Day(until.In(zone)), Label: T("detail.dwd.until")}}
	body.Blocks = []Block{{Kind: BlockTimeline, Label: T("detail.dwd.warnings"), Data: events}}

	// The DWD's full text and what to do, per warning.
	for _, w := range shown.Warnings {
		if w.Description == "" {
			continue
		}
		read := Reading{Title: w.Headline, Text: []string{w.Description}}
		if w.Instruction != "" {
			read.Text = append(read.Text, w.Instruction)
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockRead, Data: read})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: head, Body: body}
}

// energyMonths: consumption of the last twelve months against the year
// before (dashed), from the recorded days.
func energyMonths(h *metrics.History, now time.Time) (Block, bool) {
	all := metrics.MonthSums(h, metrics.EnergyKWhKey, now, 2*monthsPerYear)
	prev, this := all[:monthsPerYear], all[monthsPerYear:]
	if !hasValues(this) {
		return Block{}, false
	}
	g := Graph{Kind: GraphCols, Mark: -1, Series: []Series{{Values: this, Class: "s1", Label: T("detail.energy.months_this")}}}
	if hasValues(prev) {
		g.Series = append(g.Series, Series{Values: prev, Class: "s1", Label: T("detail.energy.months_prev")})
	}
	g.Ticks = []any{now.AddDate(0, 1-monthsPerYear, 0).Format(monthTick), now.Format(monthTick)}
	return Block{Kind: BlockGraph, Label: T("detail.energy.months"), Data: g}, true
}

// monthTick labels a month under a chart: "10/26".
const monthTick = "01/06"

// energyDetail (time): prices of today and tomorrow, the cheap window,
// consumption per day.
func energyDetail(cfg EnergyConfig, results map[string]any, _ ViewCtx) DetailView {
	raw, ok := results["data"].(*sources.TibberDataset)
	if !ok {
		return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
	}
	now := time.Now()
	data := energyPrices(raw, cfg, now)
	cents := func(v float64) any { return NumU(v*centsPerUnit, 1, "ct") }
	body := &DetailBody{Line: []Fact{{Label: T("detail.energy.now"), Value: cents(data.Current)}, {Label: T("detail.energy.level"), Value: data.Level}}}
	start, avg, cheap := metrics.CheapWindow(data.Prices, now, cfg.CheapHours)
	if cheap {
		body.Line = append(body.Line, Fact{Label: textArgs("detail.energy.cheapest", "n", cfg.CheapHours),
			Value: TxtA("detail.energy.window", "time", start.In(clockZone()).Format(timeOfDay), "price", cents(avg))})
	}
	if len(data.Prices) >= minPoints {
		window := cheapWindow{}
		if cheap {
			window = cheapWindow{from: start, hours: cfg.CheapHours}
		}
		body.Blocks = append(body.Blocks, energyGraph(data.Prices, now, window))
		low, high := slices.MinFunc(data.Prices, byTotal), slices.MaxFunc(data.Prices, byTotal)
		body.Facts = []Kpi{{Value: cents(data.Current), Label: T("detail.energy.now")}, {Value: cents(low.Total), Label: T("detail.energy.low"), Tier: "green"},
			{Value: cents(high.Total), Label: T("detail.energy.high"), Tier: "red"}}
	}
	var rows [][]Cell
	cost, _ := metrics.EnergyTotals(data.Days)
	for _, d := range slices.Backward(data.Days) {
		temp := any("–")
		if d.HasTemp {
			temp = NumU(d.TempC, 0, "°C")
		}
		rows = append(rows, []Cell{{Value: DayS(d.Day)}, {Value: Num(d.KWh, 1)}, {Value: Money(d.Cost, data.Currency)}, {Value: temp}})
	}
	if len(rows) > 0 {
		body.Facts = append(body.Facts, Kpi{Value: Money(cost, data.Currency), Label: textDays("detail.energy.cost", len(rows))})
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.energy.days"),
			Data: Table{Head: []Text{T("detail.energy.day"), T("detail.energy.kwh"), T("detail.energy.cost_col"), T("detail.energy.temp")}, Rows: rows, Num: []int{1, 2, 3}}})
	}
	if b, ok := energyMonths(historyOf(results), now); ok {
		body.Blocks = append(body.Blocks, b)
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Head: DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: raw.URL, Primary: true}}}, Body: body}
}

// byTotal orders prices.
func byTotal(a, b sources.PricePoint) int { return cmp.Compare(a.Total, b.Total) }

// cheapWindow is the cheapest run of hours; zero is none.
type cheapWindow struct {
	from  time.Time
	hours int
}

// energyGraph: one column per hour; the cheap window green, the dearest
// quarter yellow, a line for now.
func energyGraph(prices []sources.PricePoint, now time.Time, window cheapWindow) Block {
	values := make([]float64, len(prices))
	for i, p := range prices {
		values[i] = p.Total * centsPerUnit
	}
	sorted := slices.Sorted(slices.Values(values))
	dear := sorted[len(sorted)-len(sorted)/pricesQuarter-1]
	g := ColGraph(values, "s1")
	g.States = make([]string, len(prices))
	cheapTo := window.from.Add(time.Duration(window.hours) * time.Hour)
	for i, p := range prices {
		switch {
		case window.hours > 0 && !p.At.Before(window.from) && p.At.Before(cheapTo):
			g.States[i] = "ok"
		case values[i] > dear:
			g.States[i] = "warn"
		}
		if !p.At.After(now) && p.At.Add(time.Hour).After(now) {
			g.Mark = i
		}
	}
	zone := clockZone()
	first, last := prices[0].At.In(zone), prices[len(prices)-1].At.In(zone)
	g.Ticks = []any{first.Format(timeOfDay), last.Format(timeOfDay)}
	if first.Format(isoDate) != last.Format(isoDate) {
		g.Ticks = []any{Day(first), Day(last)}
	}
	return Block{Kind: BlockGraph, Label: T("detail.energy.prices"), Meta: Txt("detail.energy.ct"), Hero: true, Data: g}
}

// grocyDetail (record without the facts column): stock, shopping, chores.
func grocyDetail(cfg GrocyConfig, data *sources.GrocyDataset, ctx ViewCtx, results map[string]any) DetailView {
	shown, _ := grocyView(cfg, data, ctx)["Data"].(*sources.GrocyDataset)
	var stock [][]Cell
	for _, p := range shown.Expired {
		stock = append(stock, []Cell{{Value: p.Name}, {Value: DayS(p.Due), State: "bad"}})
	}
	for _, p := range shown.Overdue {
		stock = append(stock, []Cell{{Value: p.Name}, {Value: DayS(p.Due), State: "bad"}})
	}
	for _, p := range shown.Soon {
		stock = append(stock, []Cell{{Value: p.Name}, {Value: DayS(p.Due), State: "warn"}})
	}
	var missing [][]Cell
	for _, p := range shown.Missing {
		missing = append(missing, []Cell{{Value: p.Name}, {Value: Num(p.Missing, 0)}})
	}
	lateChores := metrics.GrocyLateChores(shown, todayOf(ctx))
	late := len(lateChores)
	var chores []LitRow
	for _, c := range shown.Chores {
		chores = append(chores, LitRow{Name: c.Name, Meta: Day(c.Due), State: cmp.Or(stateIf(slices.Contains(lateChores, c), "warn"), "ok")})
	}
	expired := len(metrics.GrocyPastDue(shown))
	body := &DetailBody{
		Line: []Fact{{Label: T("detail.grocy.expired"), Value: expired, State: stateIf(expired > 0, "bad")}, {Label: T("detail.grocy.soon"), Value: len(shown.Soon)},
			{Label: T("detail.grocy.missing"), Value: len(shown.Missing)}, {Label: T("detail.grocy.chores"), Value: late, State: stateIf(late > 0, "warn")}},
		Facts: []Kpi{{Value: expired, Label: T("detail.grocy.expired"), Tier: tierIf(expired > 0, "red", "")}, {Value: len(shown.Soon), Label: T("detail.grocy.soon"), Tier: tierIf(len(shown.Soon) > 0, "yellow", "")},
			{Value: len(shown.Missing), Label: T("detail.grocy.missing")}, {Value: late, Label: T("detail.grocy.chores"), Tier: tierIf(late > 0, "yellow", "")}},
	}
	var pair []Block
	if len(stock) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.grocy.stock"), Data: Table{Head: []Text{T("detail.grocy.product"), T("detail.grocy.due")}, Rows: stock}})
	}
	if len(missing) > 0 {
		pair = append(pair, Block{Kind: BlockTable, Label: T("detail.grocy.below_min"), Data: Table{Head: []Text{T("detail.grocy.product"), T("detail.grocy.amount")}, Rows: missing, Num: []int{1}}})
	}
	body.Blocks = pairOf(pair)
	if len(missing) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTasks, Data: Tasks{Items: []Task{{Text: TxtA("detail.grocy.to_list", "n", len(shown.Missing)),
			State: "warn", Action: T("detail.grocy.add_missing"), Do: "shopping_add"}}}})
	}
	if len(chores) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("grocy.chores"), Data: chores})
	}
	if len(body.Blocks) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.grocy.none")}}
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if expired > 0 {
		head.State, head.StateKey, head.StateArgs = "bad", "detail.grocy.n_expired", map[string]any{"n": expired}
	}
	return DetailView{Head: head, Body: body}
}

// hassDetail (wall): one card per entity with its value and last change.
func hassDetail(cfg HassConfig, data *sources.HassDataset, ctx ViewCtx, results map[string]any) DetailView {
	rows, _ := hassView(cfg, data, ctx)["Rows"].([]HassRow)
	var cards []Card
	var table [][]Cell
	trouble := 0
	for _, r := range rows {
		card := Card{Label: Plain(r.Name), Value: strings.TrimSpace(r.Value + " " + r.Unit)}
		changed := any("–")
		if e, found := data.Find(r.ID); found {
			changed = agoOf(e.Changed)
		}
		switch {
		case r.Missing:
			card.Value, card.Tier = "–", "red"
		case r.Low || r.Level == levelFail:
			card.Tier = "red"
		case r.Level == levelWarn:
			card.Tier = "yellow"
		}
		if card.Tier != "" {
			trouble++
		}
		card.Sub = changed
		cards = append(cards, card)
		table = append(table, []Cell{{Value: r.Name}, {Value: r.ID}, {Value: card.Value, State: tierState(card.Tier)}, {Value: changed}})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockWall, Data: cards},
		{Kind: BlockTable, Label: T("detail.hass.entities"), Data: Table{Head: []Text{T("detail.hass.name"), T("detail.hass.id"), T("detail.hass.value"), T("detail.hass.changed")}, Rows: table}}}}
	if h, ok := results[openName].(*sources.HassHistory); ok {
		body.Blocks = append(body.Blocks, hassHistory(rows, h, time.Now())...)
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	head := DetailHead{State: "ok", StateKey: "detail.hass.fine", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if trouble > 0 {
		head.State, head.StateKey, head.StateArgs = "warn", "detail.hass.trouble", map[string]any{"n": trouble}
	}
	return DetailView{Head: head, Body: body}
}

// hassHistory: the last day per entity, hour by hour; numbers as a line,
// switches and sensors as a strip (on = ok).
//
//	Wohnzimmer °C  ╱‾‾╲__╱‾
//	Büro Licht     ░░▓▓▓▓▓░░
func hassHistory(rows []HassRow, h *sources.HassHistory, now time.Time) []Block {
	start := now.Add(-sources.HassHistoryHours * time.Hour)
	var out []Block
	var strips []Strip
	for _, r := range rows {
		points := h.ByID[r.ID]
		if len(points) == 0 {
			continue
		}
		states := make([]string, sources.HassHistoryHours)
		for i := range states {
			end := start.Add(time.Duration(i+1) * time.Hour)
			for _, p := range points {
				if !p.At.After(end) {
					states[i] = p.State
				}
			}
		}
		if values, numeric := hassNumbers(states); numeric {
			g := LineGraph(Series{Values: values, Class: "s1"})
			g.Ticks = []any{start.In(clockZone()).Format(timeOfDay), now.In(clockZone()).Format(timeOfDay)}
			out = append(out, Block{Kind: BlockGraph, Label: Plain(strings.TrimSpace(r.Name + " " + r.Unit)), Data: g})
			continue
		}
		strip := Strip{Name: r.Name, States: make([]string, len(states)), Value: r.Value}
		for i, st := range states {
			strip.States[i] = "off"
			if st == sources.HassOn {
				strip.States[i] = "ok"
			}
		}
		strips = append(strips, strip)
	}
	if len(strips) > 0 {
		out = append(out, Block{Kind: BlockStrips, Label: T("detail.hass.day"), Data: strips})
	}
	return out
}

// hassNumbers reads hourly states as numbers; numeric is false when a
// known state is no number. An hour without a state is a Gap.
func hassNumbers(states []string) ([]float64, bool) {
	values := make([]float64, len(states))
	seen := false
	for i, st := range states {
		values[i] = Gap
		if st == "" || st == sources.HassUnavailable {
			continue
		}
		v, err := strconv.ParseFloat(st, 64)
		if err != nil {
			return nil, false
		}
		values[i], seen = v, true
	}
	return values, seen
}

// stormGusts (km/h) are gusts that matter outdoors: Beaufort 8, gale.
const stormGusts = 62

// clockPart is the time of a local ISO time: "2026-10-03T07:21" → "07:21";
// "" stays "–".
func clockPart(iso string) string {
	if _, t, ok := strings.Cut(iso, "T"); ok {
		return t
	}
	return "–"
}

// weatherDetail (wall): now, rain and the next days as cards, the next
// hours as charts.
func weatherDetail(cfg WeatherConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := weatherView(cfg, results, ctx)
	data, ok := results["weather"].(*sources.WeatherResult)
	if !ok || data == nil {
		return DetailView{Body: &DetailBody{Blocks: []Block{{Kind: BlockText, Data: Txt("detail.weather.none")}}}}
	}
	unit := "°C"
	conv := func(c float64) float64 { return c }
	if cfg.Fahrenheit {
		unit, conv = "°F", fahrenheit
	}
	temps := make([]float64, len(data.Hours))
	rain := make([]float64, len(data.Hours))
	for i, h := range data.Hours {
		temps[i], rain[i] = conv(h.Temp), h.Rain
	}
	cards := []Card{{Label: T("detail.weather.now"), Value: NumU(conv(data.Temp), 0, unit), Spark: temps}, {Label: T("detail.weather.wind"), Value: NumU(data.Wind, 0, "km/h"),
		Sub: TxtA("detail.weather.gusts", "n", NumU(data.Gusts, 0, "km/h")), Tier: tierIf(data.Gusts >= stormGusts, "yellow", "")}}
	if len(rain) > 0 {
		wet := slices.Max(rain)
		card := Card{Label: T("detail.weather.rain"), Value: NumU(wet, 0, "%"), Spark: rain, Tier: tierIf(wet >= rainLikely, "cyan", "")}
		if from, found := view["RainFrom"].(string); found {
			card.Sub = TxtA("weather.rain_from", "time", from)
		}
		cards = append(cards, card)
	}
	if len(data.Days) > 0 {
		today := data.Days[0]
		cards = append(cards, Card{Label: T("detail.weather.today"), Value: TxtA("detail.weather.max_min", "max", NumU(conv(today.Max), 0, unit), "min", NumU(conv(today.Min), 0, unit)),
			Sub: TxtA("detail.weather.sun", "rise", clockPart(today.Sunrise), "set", clockPart(today.Sunset))})
	}
	body := &DetailBody{Blocks: []Block{{Kind: BlockWall, Data: cards}}}
	if hasValues(temps) {
		zone := clockZone()
		ticks := []any{Txt("detail.weather.now_tick"), Txt("detail.weather.later")}
		if t, err := time.ParseInLocation("2006-01-02T15:04", data.Hours[len(data.Hours)-1].At, zone); err == nil {
			ticks[1] = t.Format(timeOfDay)
		}
		temp := LineGraph(Series{Values: temps, Class: "s5"})
		temp.Ticks = ticks
		chance := ColGraph(rain, "s1")
		chance.Lo, chance.Hi, chance.Ticks = 0, percentScale, ticks
		body.Blocks = append(body.Blocks, pairOf([]Block{{Kind: BlockGraph, Label: T("weather.next_hours"), Meta: unit, Data: temp},
			{Kind: BlockGraph, Label: T("weather.rain_chance"), Meta: "%", Data: chance}})...)
	}
	var days [][]Cell
	for _, d := range data.Days {
		days = append(days, []Cell{{Value: DayS(d.Day)}, {Value: Txt("weather." + WeatherKind(d.Code))}, {Value: NumU(conv(d.Max), 0, unit)}, {Value: NumU(conv(d.Min), 0, unit)},
			{Value: NumU(d.Gusts, 0, "km/h"), State: stateIf(d.Gusts >= stormGusts, "warn")}, {Value: clockPart(d.Sunrise) + "–" + clockPart(d.Sunset)}})
	}
	if len(days) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.weather.days"),
			Data: Table{Head: []Text{T("detail.weather.day"), T("detail.weather.sky"), T("detail.weather.max"), T("detail.weather.min"), T("detail.weather.gusts_col"), T("detail.weather.sun_col")},
				Rows: days, Num: []int{2, 3, 4}}})
	}
	return DetailView{Body: body}
}

// marketLines draws price lines on one scale: each starts at 100.
func marketLines(names []string, lines [][]float64) (Graph, bool) {
	var series []Series
	for i, values := range lines {
		if len(values) < minPoints || values[0] == 0 {
			continue
		}
		series = append(series, Series{Values: scaled(values, percentBase/values[0]), Class: dataClass(i), Label: names[i]})
	}
	return LineGraph(series...), len(series) > 0
}

// changeKpi is a change in percent, green up and red down.
func changeKpi(v float64, label Text) Kpi {
	return Kpi{Value: NumU(v, 1, "%"), Label: label, Tier: tierIf(v >= 0, "green", "red")}
}

// cryptoDetail (record): prices, 24-hour change, the week's lines.
func cryptoDetail(cfg CryptoConfig, results map[string]any, _ ViewCtx) DetailView {
	data, ok := results["prices"].(*sources.CryptoResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	currency := strings.ToUpper(data.Currency)
	body := &DetailBody{Side: []Fact{{Label: T("detail.market.currency"), Value: currency}, {Label: T("detail.market.coins"), Value: len(data.Coins)}}}
	var rows [][]Cell
	var names []string
	var lines [][]float64
	for _, c := range data.Coins {
		body.Facts = append(body.Facts, Kpi{Value: Money(c.Price, currency), Label: Plain(c.ID)}, changeKpi(c.Change, T("detail.market.day")))
		rows = append(rows, []Cell{{Value: c.ID}, {Value: Money(c.Price, currency)}, {Value: NumU(c.Change, 1, "%"), State: stateIf(c.Change < 0, "bad")}})
		names, lines = append(names, c.ID), append(lines, c.Spark)
	}
	if g, found := marketLines(names, lines); found {
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.market.week"), Meta: Txt("detail.market.indexed"), Hero: true, Data: g})
	}

	// The month, read on open, in place of the tile's week.
	if h, found := results[openName].(*sources.CoinHistory); found {
		var month [][]float64
		for _, id := range names {
			month = append(month, h.ByID[id])
		}
		if g, found := marketLines(names, month); found {
			body.Blocks = append(body.Blocks[:0:0], Block{Kind: BlockGraph, Label: T("detail.market.month"), Meta: Txt("detail.market.indexed"), Hero: true, Data: g})
		}
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.market.coins"),
		Data: Table{Head: []Text{T("detail.market.coin"), T("detail.market.price"), T("detail.market.day")}, Rows: rows, Num: []int{1, 2}}})
	return DetailView{Body: body}
}

// stocksDetail (record): closes, day and week change, the month's lines.
func stocksDetail(cfg StocksConfig, results map[string]any, _ ViewCtx) DetailView {
	data, ok := results["quotes"].(*sources.StocksResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	body := &DetailBody{Side: []Fact{{Label: T("detail.market.symbols"), Value: len(data.Quotes)}}}
	var rows [][]Cell
	var names []string
	var lines [][]float64
	for _, q := range data.Quotes {
		body.Facts = append(body.Facts, changeKpi(q.Change, textArgs("detail.market.day_of", "name", q.Symbol)))
		rows = append(rows, []Cell{{Value: q.Symbol}, {Value: Money(q.Close, q.Currency)}, {Value: NumU(q.Change, 1, "%"), State: stateIf(q.Change < 0, "bad")},
			{Value: NumU(q.WeekChange, 1, "%"), State: stateIf(q.WeekChange < 0, "bad")}})
		names, lines = append(names, q.Symbol), append(lines, q.Closes)
	}
	if g, found := marketLines(names, lines); found {
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.market.month"), Meta: Txt("detail.market.indexed"), Hero: true, Data: g})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("detail.market.symbols"),
		Data: Table{Head: []Text{T("detail.market.symbol"), T("detail.market.close"), T("detail.market.day"), T("detail.market.week_col")},
			Rows: rows, Num: []int{1, 2, 3}}})
	return DetailView{Body: body}
}

// ratesDetail (record): each rate both ways with its change.
func ratesDetail(cfg RatesConfig, results map[string]any, _ ViewCtx) DetailView {
	data, ok := results["rates"].(*sources.RatesResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	body := &DetailBody{Side: []Fact{{Label: T("detail.rates.base"), Value: data.Base}, {Label: T("detail.rates.as_of"), Value: DayS(data.Day)}}}
	var rows [][]Cell
	for _, r := range data.Rates {
		body.Facts = append(body.Facts, Kpi{Value: Num(r.Value, rateDigits), Label: Plain(r.Code)})
		rows = append(rows, []Cell{{Value: r.Code}, {Value: Num(r.Value, rateDigits)}, {Value: Num(r.Inverse, rateDigits)}, {Value: NumU(r.Change, 2, "%"), State: stateIf(r.Change < 0, "bad")}})
	}
	body.Blocks = []Block{{Kind: BlockTable, Label: T("detail.rates.list"),
		Data: Table{Head: []Text{T("detail.rates.code"), textArgs("detail.rates.per_base", "base", data.Base), textArgs("detail.rates.in_base", "base", data.Base), T("detail.rates.change")},
			Rows: rows, Num: []int{1, 2, 3}}}}

	// The last three months, read on open, indexed to their first day.
	if h, found := results[openName].(*sources.RatesHistory); found && len(h.Days) > 0 {
		var names []string
		var lines [][]float64
		for _, r := range data.Rates {
			names, lines = append(names, r.Code), append(lines, h.ByCode[r.Code])
		}
		if g, found := marketLines(names, lines); found {
			g.Ticks = []any{DayS(h.Days[0]), DayS(h.Days[len(h.Days)-1])}
			body.Blocks = append([]Block{{Kind: BlockGraph, Label: T("detail.rates.history"), Meta: Txt("detail.market.indexed"), Hero: true, Data: g}}, body.Blocks...)
		}
	}
	return DetailView{Body: body}
}

// rateDigits is how many decimals a rate shows.
const rateDigits = 4

// boardTable is a departure board as a table.
func boardTable(rows []MoveRow, label Text) Block {
	var cells [][]Cell
	for _, r := range rows {
		status := Cell{Value: r.Status}
		switch {
		case r.Canceled:
			status = Cell{Value: Txt("board.canceled"), State: "bad"}
		case r.Delay > 0:
			status = Cell{Value: NumU(float64(r.Delay), 0, "min"), State: "warn"}
		}
		cells = append(cells, []Cell{{Value: r.Time}, {Value: r.Line}, {Value: r.Place}, {Value: r.Platform}, status})
	}
	return Block{Kind: BlockTable, Label: label, Hero: true,
		Data: Table{Head: []Text{T("detail.board.time"), T("detail.board.line"), T("detail.board.place"), T("detail.board.platform"), T("detail.board.status")}, Rows: cells}}
}

// flightsDetail (record): the board with the delayed and cancelled.
func flightsDetail(cfg FlightsConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := flightsView(cfg, results, ctx)
	rows, _ := view["Rows"].([]MoveRow)
	canceled := 0
	for _, r := range rows {
		if r.Canceled {
			canceled++
		}
	}
	body := &DetailBody{Side: []Fact{{Label: T("detail.board.airport"), Value: cfg.Airport}, {Label: T("detail.board.direction"), Value: Txt("detail.board." + strings.ToLower(cfg.Direction))}},
		Facts:  []Kpi{{Value: len(rows), Label: T("detail.board.flights")}, {Value: canceled, Label: T("detail.board.canceled"), Tier: tierIf(canceled > 0, "red", "")}},
		Blocks: []Block{boardTable(rows, T("detail.board.flights"))}}
	return DetailView{Body: body}
}

// transitDetail (time): the departures and when to leave for the first.
func transitDetail(cfg TransitConfig, results map[string]any, ctx ViewCtx) DetailView {
	view := transitView(cfg, results, ctx)
	rows, _ := view["Rows"].([]MoveRow)
	stop, _ := view["Stop"].(string)
	body := &DetailBody{Line: []Fact{{Label: T("detail.board.stop"), Value: cmp.Or(stop, cfg.Stop)}}}
	if cfg.Walk > 0 {
		body.Line = append(body.Line, Fact{Label: T("detail.board.walk"), Value: NumU(float64(cfg.Walk), 0, "min")})
	}
	delayed := 0
	for _, r := range rows {
		if r.Delay > 0 || r.Canceled {
			delayed++
		}
	}
	if len(rows) > 0 {
		if at, err := time.ParseInLocation(timeOfDay, rows[0].Time, clockZone()); err == nil {
			leave := at.Add(time.Duration(rows[0].Delay-cfg.Walk) * time.Minute)
			body.Facts = append(body.Facts, Kpi{Value: leave.Format(timeOfDay), Label: T("detail.board.leave"), Tier: "cyan"})
		}
	}
	if len(rows) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("detail.board.none")}}
		return DetailView{Body: body}
	}
	body.Facts = append(body.Facts, Kpi{Value: delayed, Label: T("detail.board.delayed"), Tier: tierIf(delayed > 0, "yellow", "")})
	body.Blocks = []Block{boardTable(rows, T("detail.board.departures"))}

	// Disruption notes, once per text with the lines they touch.
	var texts []string
	lines := map[string][]string{}
	for _, r := range rows {
		for _, text := range r.Remarks {
			if _, ok := lines[text]; !ok {
				texts = append(texts, text)
			}
			if !slices.Contains(lines[text], r.Line) {
				lines[text] = append(lines[text], r.Line)
			}
		}
	}
	var notes []LitRow
	for _, text := range texts {
		notes = append(notes, LitRow{Name: text, Meta: strings.Join(lines[text], ", "), State: "warn"})
	}
	if len(notes) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockRows, Label: T("detail.board.remarks"), Data: notes})
	}
	return DetailView{Body: body}
}

// holidaysDetail (grid): half a year as weeks with holidays and bridge
// days, the coming holidays.
func holidaysDetail(cfg HolidaysConfig, results map[string]any, ctx ViewCtx) DetailView {
	data, ok := results["days"].(*sources.HolidaysResult)
	if !ok {
		return DetailView{Body: &DetailBody{}}
	}
	today := todayOf(ctx)
	first := weekStart(today)
	last := first.AddDate(0, 0, weekDays*holidayDetailWks-1)
	level := map[string]int{}
	var events []Event
	bridges := 0
	for _, h := range data.Days {
		at, err := time.Parse(isoDate, h.Day)
		if err != nil || at.Before(today) || at.After(last) {
			continue
		}
		level[h.Day] = holidayLevel
		ev := Event{At: at, Title: h.Name, State: TxtA("holidays.in", "days", int(at.Sub(today).Hours()/hoursPerDayInsight)), Tier: "cyan"}
		if b := bridgeDay(at); b != "" {
			level[b] = max(level[b], bridgeLevel)
			ev.Sub = TxtA("holidays.bridge", "day", DayS(b))
			bridges++
		}
		events = append(events, ev)
	}
	// Days off booked in Kimai (holiday plugin) join the raster.
	var absent map[time.Time]bool
	if kimai, found := results[peerKimai].(*sources.KimaiDataset); found {
		absent = metrics.AbsentDays(kimai)
	}
	off := 0
	heat := Heat{Rows: weekDays, Ticks: []any{Day(first), Day(last)}}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		l := level[d.Format(isoDate)]
		if absent[d] && !d.Before(today) {
			l, off = max(l, absentLevel), off+1
		}
		heat.Levels = append(heat.Levels, l)
	}
	body := &DetailBody{Line: []Fact{{Label: T("detail.holidays.region"), Value: cmp.Or(cfg.State, cfg.Country)}}}
	if absent != nil {
		body.Line = append(body.Line, Fact{Label: T("detail.holidays.absent"), Value: off})
	}
	if len(events) > 0 {
		body.Facts = []Kpi{{Value: events[0].State, Label: Plain(events[0].Title.(string)), Tier: "cyan"}, {Value: len(events), Label: T("detail.holidays.count")},
			{Value: bridges, Label: T("detail.holidays.bridges")}}
	}
	body.Blocks = []Block{{Kind: BlockHeat, Label: T("detail.holidays.raster"), Meta: Txt("detail.holidays.legend"), Hero: true, Data: heat}}
	if len(events) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTimeline, Label: T("detail.holidays.next"), Data: events})
	}
	return DetailView{Body: body}
}
