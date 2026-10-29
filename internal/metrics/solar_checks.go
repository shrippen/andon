package metrics

// What the PV checks compare (rules solar.*, cross.solar_selfuse, the
// homelab's power price):
//
//	ExpectedYield   a day's forecast as recorded, else the same days last year
//	DarkHours       hours of today with forecast but no yield
//	SelfRateYearAgo this month's self-use rate and last year's same month
//	GridShare       the share of consumption still bought (1 − autarky)
//	ForecastLeft    the yield still expected today

import (
	"time"

	"andon/internal/sources"
)

// Expect says where an expected yield comes from.
type Expect int

const (
	ExpectNone     Expect = iota
	ExpectForecast        // the forecast recorded that day
	ExpectLastYear        // the mean of the same days last year
)

// Spans of the comparisons.
const (
	yearAgoSpread  = 3  // days either side of the date last year
	yearAgoMinDays = 3  // last year's days needed for a mean
	gridDays       = 30 // days the grid share is averaged over
)

// ExpectedYield is what day ("2026-10-28") should have made: its
// forecast as the analysis recorded it, else the mean of the same date
// ±3 days last year.
func ExpectedYield(e *sources.HassEnergy, h *History, day string) (float64, Expect) {
	for _, p := range h.SeriesOf(SolarForecastKey) {
		if p.Day.Format(time.DateOnly) == day {
			return p.Value, ExpectForecast
		}
	}

	t, ok := ParseDay(day)
	if !ok || e == nil {
		return 0, ExpectNone
	}
	sum, n := 0.0, 0
	for d := -yearAgoSpread; d <= yearAgoSpread; d++ {
		if past, ok := SolarOn(e, t.AddDate(-1, 0, d).Format(time.DateOnly)); ok {
			sum += past.Solar
			n++
		}
	}
	if n < yearAgoMinDays {
		return 0, ExpectNone
	}
	return sum / float64(n), ExpectLastYear
}

// DarkHours counts the hours of today before now with at least minKWh
// forecast and no yield. Only hours the curve covers count: a curve that
// stops early is late, not dark.
func DarkHours(e *sources.HassEnergy, now time.Time, minKWh float64) int {
	hours := SolarHours(e)
	if len(hours) == 0 {
		return 0
	}
	made := map[int64]float64{} // by Unix second: equal instants in other zones are other keys
	for _, h := range hours {
		made[h.At.Unix()] = h.KWh
	}
	last := hours[len(hours)-1].At
	today := SolarDayKey(e, now)

	n := 0
	for _, f := range e.Forecast {
		if SolarDayKey(e, f.At) != today || f.At.After(last) || f.KWh < minKWh {
			continue
		}
		if made[f.At.Unix()] <= 0 {
			n++
		}
	}
	return n
}

// SelfRateYearAgo is the self-use rate of now's month so far and of the
// same month last year; false without both.
func SelfRateYearAgo(e *sources.HassEnergy, now time.Time) (this, before float64, ok bool) {
	local := now.In(e.Location())
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	m, ok1 := SolarMonth(e, month.Format(solarMonthLayout))
	p, ok2 := SolarMonth(e, month.AddDate(-1, 0, 0).Format(solarMonthLayout))
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	this, ok1 = SolarSum(e, []sources.SolarDay{m}).SelfRate()
	before, ok2 = SolarSum(e, []sources.SolarDay{p}).SelfRate()
	return this, before, ok1 && ok2
}

// solarMonthLayout is a statistics month: "2026-10".
const solarMonthLayout = "2006-01"

// SolarMonthAt is the figures of t's month in Home Assistant's zone.
func SolarMonthAt(e *sources.HassEnergy, t time.Time) (sources.SolarDay, bool) {
	return SolarMonth(e, t.In(e.Location()).Format(solarMonthLayout))
}

// GridShare is the share of the consumption bought from the grid over
// the last 30 full days (1 − autarky); 1 without PV statistics.
func GridShare(e *sources.HassEnergy, now time.Time) float64 {
	today := SolarDayKey(e, now)
	from := SolarDayKey(e, now.AddDate(0, 0, -gridDays))
	var days []sources.SolarDay
	if e != nil {
		for _, d := range e.Days {
			if d.Day >= from && d.Day < today {
				days = append(days, d)
			}
		}
	}
	aut, ok := SolarSum(e, days).Autarky()
	if !ok {
		return 1
	}
	return 1 - aut
}

// ForecastLeft is the yield still expected today after now: the
// forecast hours that have not begun.
func ForecastLeft(e *sources.HassEnergy, now time.Time) (float64, bool) {
	if e == nil || len(e.Forecast) == 0 {
		return 0, false
	}
	today := SolarDayKey(e, now)
	sum := 0.0
	for _, f := range e.Forecast {
		if SolarDayKey(e, f.At) == today && !f.At.Before(now) {
			sum += f.KWh
		}
	}
	return sum, true
}
