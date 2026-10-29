package sources

// The demo's PV figures (power.hass_energy): the world gives typical
// month yields, daylight and a weather cycle; the days, months, today's
// curve and the forecast follow from them, relative to today.
//
//	day yield = month_kwh / days in month × weather × (garage down: 1 − garage_share)
//	the day:  direct use, battery (until battery_idle), export, import
//	the curve: a sine arc of daylight hours around noon

import (
	"math"
	"time"

	"andon/internal/sources/demoworld"
)

// demoSolarStep is the step of today's curve, as Home Assistant's
// 5-minute statistics.
const demoSolarStep = 5 * time.Minute

// demoSolar is the world's parameters.
type demoSolar struct {
	HassEnergy
	MonthKWh, Daylight, Weather   []float64
	Noon, UseKWh, DirectShare     float64
	BatteryKWh, BatteryEfficiency float64
	GarageShare                   float64
	GarageDown, BatteryIdle       string // "2026-10-27"
}

func DemoHassEnergy(now time.Time) *HassEnergy {
	var p demoSolar
	demoworld.MustDecode("power.hass_energy", now, &p)
	data := p.HassEnergy
	loc := data.Location()
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	// Every day of the months read, oldest first; today so far.
	first := time.Date(local.Year(), local.Month()-energyMonthsBack+1, 1, 0, 0, 0, 0, loc)
	var days []SolarDay
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		share := 1.0
		if d.Equal(today) {
			share = p.arc(d, p.hourOf(local))
		}
		days = append(days, p.day(d, share))
	}
	data.Days = days[max(0, len(days)-energyDaysBack-1):]
	data.Months = demoMonths(days)

	// Today per 5 minutes up to now, the forecast per hour today and
	// tomorrow (without the garage: the forecast does not know).
	full := p.yield(today) * p.garage(today)
	for at := today; !at.Add(demoSolarStep).After(local); at = at.Add(demoSolarStep) {
		data.Today = append(data.Today, SolarPoint{At: at, KWh: full * p.span(today, at, demoSolarStep)})
	}
	for d := range 2 {
		day := today.AddDate(0, 0, d)
		for at := day; at.Before(day.AddDate(0, 0, 1)); at = at.Add(time.Hour) {
			if kwh := p.yield(day) * p.span(day, at, time.Hour); kwh > 0 {
				data.Forecast = append(data.Forecast, SolarPoint{At: at, KWh: kwh})
			}
		}
	}
	return &data
}

// yield is the day's yield of all panels in good order.
func (p demoSolar) yield(d time.Time) float64 {
	month := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	epochDay := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).Unix() / int64((24 * time.Hour).Seconds())
	return p.MonthKWh[d.Month()-1] / float64(month) * p.Weather[epochDay%int64(len(p.Weather))]
}

// garage is the share of the panels that deliver on day d.
func (p demoSolar) garage(d time.Time) float64 {
	if d.Format(time.DateOnly) >= p.GarageDown {
		return 1 - p.GarageShare
	}
	return 1
}

// day splits a day's yield (share of it so far) into direct use,
// battery, export and import.
func (p demoSolar) day(d time.Time, share float64) SolarDay {
	solar := p.yield(d) * p.garage(d) * share
	use := p.UseKWh * share
	direct := math.Min(solar*p.DirectShare, use)
	charge := 0.0
	if d.Format(time.DateOnly) < p.BatteryIdle {
		charge = math.Min(p.BatteryKWh*share, solar-direct)
	}
	discharge := math.Min(charge*p.BatteryEfficiency, use-direct)
	return SolarDay{Day: d.Format(time.DateOnly), Solar: solar, Export: solar - direct - charge,
		Import: use - direct - discharge, Charge: charge, Discharge: discharge}
}

// hourOf is a time's hour of its day: 13:30 → 13.5.
func (p demoSolar) hourOf(t time.Time) float64 {
	return float64(t.Hour()) + float64(t.Minute())/60
}

// arc is the share of day d's yield made before hour h: a sine arc of
// the month's daylight around noon.
func (p demoSolar) arc(d time.Time, h float64) float64 {
	light := p.Daylight[d.Month()-1]
	t := min(max(h-(p.Noon-light/2), 0), light)
	return (1 - math.Cos(math.Pi*t/light)) / 2
}

// span is the share of day d's yield made in [at, at+step).
func (p demoSolar) span(d, at time.Time, step time.Duration) float64 {
	h := p.hourOf(at)
	return p.arc(d, h+step.Hours()) - p.arc(d, h)
}

// demoMonths adds the days up per month, oldest first.
func demoMonths(days []SolarDay) []SolarDay {
	var out []SolarDay
	for _, d := range days {
		key := d.Day[:len(monthLayout)]
		if len(out) == 0 || out[len(out)-1].Day != key {
			out = append(out, SolarDay{Day: key})
		}
		m := &out[len(out)-1]
		m.Solar, m.Import, m.Export = m.Solar+d.Solar, m.Import+d.Import, m.Export+d.Export
		m.Charge, m.Discharge = m.Charge+d.Charge, m.Discharge+d.Discharge
	}
	return out
}
