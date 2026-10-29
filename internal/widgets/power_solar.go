package widgets

// The power dialog's PV part from Home Assistant's energy statistics:
// today's hours against the forecast, the last 30 days and the last two
// years per month (yield, self use, export), and the months as a table.

import (
	"math"
	"slices"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Spans of the PV charts.
const (
	solarDays   = 30
	solarMonths = 2 * monthsPerYear
)

// solarFacts are the dialog's PV figures: power now, today against the
// forecast, this month's self-use rate and autarky.
func solarFacts(e *sources.HassEnergy, now time.Time) []Kpi {
	var out []Kpi
	if kw, _, ok := metrics.SolarPower(e); ok {
		out = append(out, Kpi{Value: NumU(kw, 2, "kW"), Label: T("power.pv_now"), Tier: "green"})
	}
	today := metrics.SolarDayKey(e, now)
	d, _ := metrics.SolarOn(e, today)
	out = append(out, Kpi{Value: NumU(d.Solar, 1, "kWh"), Label: T("power.pv_today")})
	if fc, ok := metrics.ForecastOn(e, today); ok {
		out = append(out, Kpi{Value: NumU(fc, 1, "kWh"), Label: T("power.pv_forecast")})
	}
	month, _ := metrics.SolarMonth(e, today[:len(solarMonthKey)])
	span := metrics.SolarSum([]sources.SolarDay{month})
	if rate, ok := span.SelfRate(); ok {
		out = append(out, Kpi{Value: NumU(rate*percentScale, 0, "%"), Label: T("power.self_month")})
	}
	if aut, ok := span.Autarky(); ok {
		out = append(out, Kpi{Value: NumU(aut*percentScale, 0, "%"), Label: T("power.autarky_month")})
	}
	return out
}

// solarBlocks are the PV charts and the month table.
func solarBlocks(e *sources.HassEnergy, now time.Time) []Block {
	var out []Block
	if b, ok := solarToday(e, now); ok {
		out = append(out, b)
	}
	if b, ok := solarSpan(e.Days, dayKeys(e, now), "power.pv_days", dayLabel); ok {
		out = append(out, b)
	}
	if b, ok := solarSpan(e.Months, monthKeys(e, now), "power.pv_months", monthLabel); ok {
		out = append(out, b)
	}
	if len(e.Months) > 0 {
		out = append(out, solarTable(e.Months))
	}
	return out
}

// solarToday: today's yield per hour against the forecast, in Home
// Assistant's zone.
func solarToday(e *sources.HassEnergy, now time.Time) (Block, bool) {
	loc := e.Location()
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	yield, forecast := hourValues(metrics.SolarHours(e), start), hourValues(e.Forecast, start)
	if !hasValues(yield) && !hasValues(forecast) {
		return Block{}, false
	}

	// Hours not yet come have no yield, not zero.
	for h := local.Hour() + 1; h < hoursPerDay; h++ {
		yield[h] = Gap
	}
	g := LineGraph(Series{Values: yield, Class: "s1", Label: Txt("power.pv_yield")})
	if hasValues(forecast) {
		g.Series = append(g.Series, Series{Values: forecast, Class: "s2", Label: Txt("power.pv_forecast")})
	}
	g.Mark, g.Unit = local.Hour(), "kWh"
	for h := range hoursPerDay {
		g.Labels = append(g.Labels, start.Add(time.Duration(h)*time.Hour).Format(timeOfDay))
	}
	g.Ticks = []any{g.Labels[0], g.Labels[hoursPerDay-1]}
	return Block{Kind: BlockGraph, Label: T("power.pv_hours"), Hero: true, Data: g}, true
}

// hourValues puts a curve into the hours of the day from start on.
func hourValues(points []sources.SolarPoint, start time.Time) []float64 {
	out := make([]float64, hoursPerDay)
	for _, p := range points {
		if h := int(p.At.Sub(start).Hours()); h >= 0 && h < hoursPerDay {
			out[h] += p.KWh
		}
	}
	return out
}

// solarSpan: yield, self use and export per day or month, keys oldest
// first; a key without statistics is a gap.
func solarSpan(rows []sources.SolarDay, keys []string, label string, tick func(string) any) (Block, bool) {
	byKey := map[string]sources.SolarDay{}
	for _, r := range rows {
		byKey[r.Day] = r
	}
	yield, self, export := make([]float64, len(keys)), make([]float64, len(keys)), make([]float64, len(keys))
	var labels []any
	for i, k := range keys {
		labels = append(labels, tick(k))
		r, ok := byKey[k]
		if !ok {
			yield[i], self[i], export[i] = Gap, Gap, Gap
			continue
		}
		span := metrics.SolarSum([]sources.SolarDay{r})
		yield[i], self[i], export[i] = span.Yield, span.SelfUse(), r.Export
	}
	if !hasValues(yield) {
		return Block{}, false
	}
	g := LineGraph(Series{Values: yield, Class: "s1", Label: Txt("power.pv_yield")},
		Series{Values: self, Class: "s2", Label: Txt("power.pv_self")},
		Series{Values: export, Class: "s3", Label: Txt("power.pv_export")})
	g.Unit, g.Labels, g.Ticks = "kWh", labels, []any{labels[0], labels[len(labels)-1]}
	return Block{Kind: BlockGraph, Label: T(label), Data: g}, true
}

// dayKeys are the last solarDays days in Home Assistant's zone.
func dayKeys(e *sources.HassEnergy, now time.Time) []string {
	out := make([]string, solarDays)
	for i := range out {
		out[i] = metrics.SolarDayKey(e, now.AddDate(0, 0, i-(solarDays-1)))
	}
	return out
}

// monthKeys are the last solarMonths months in Home Assistant's zone.
func monthKeys(e *sources.HassEnergy, now time.Time) []string {
	local := now.In(e.Location())
	first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]string, solarMonths)
	for i := range out {
		out[i] = first.AddDate(0, i-(solarMonths-1), 0).Format(solarMonthKey)
	}
	return out
}

// solarMonthKey is the statistics' month: "2026-10".
const solarMonthKey = "2006-01"

func dayLabel(key string) any { return DayS(key) }

func monthLabel(key string) any {
	t, err := time.Parse(solarMonthKey, key)
	if err != nil {
		return key
	}
	return t.Format(monthTick)
}

// solarTable lists the last year's months, newest first.
func solarTable(months []sources.SolarDay) Block {
	pct := func(v float64, ok bool) any {
		if !ok {
			return "–"
		}
		return NumU(v*percentScale, 0, "%")
	}
	var rows [][]Cell
	recent := months[max(0, len(months)-monthsPerYear):]
	for _, m := range slices.Backward(recent) {
		span := metrics.SolarSum([]sources.SolarDay{m})
		rate, rateOK := span.SelfRate()
		aut, autOK := span.Autarky()
		rows = append(rows, []Cell{{Value: monthLabel(m.Day)}, {Value: NumU(span.Yield, 0, "kWh")}, {Value: NumU(span.SelfUse(), 0, "kWh")},
			{Value: NumU(m.Export, 0, "kWh")}, {Value: pct(rate, rateOK)}, {Value: pct(aut, autOK)}})
	}
	return Block{Kind: BlockTable, Label: T("power.pv_table"), Data: Table{
		Head: []Text{T("power.col.month"), T("power.pv_yield"), T("power.pv_self"), T("power.pv_export"), T("power.col.self_rate"), T("power.col.autarky")},
		Rows: rows, Num: []int{1, 2, 3, 4, 5}}}
}

// solarNow is the tile's PV: W now, kWh today, this month's self-use
// rate (NaN without).
func solarNow(e *sources.HassEnergy, now time.Time) (watts, today, rate float64) {
	kw, _, _ := metrics.SolarPower(e)
	key := metrics.SolarDayKey(e, now)
	d, _ := metrics.SolarOn(e, key)
	month, _ := metrics.SolarMonth(e, key[:len(solarMonthKey)])
	rate, ok := metrics.SolarSum([]sources.SolarDay{month}).SelfRate()
	if !ok {
		rate = math.NaN()
	}
	return kw * wattsPerKilo, d.Solar, rate
}
