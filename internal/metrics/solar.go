package metrics

// PV figures from Home Assistant's energy statistics (sources.HassEnergy):
//
//	yield        what the panels made
//	self use     yield − export (what stayed in the house or the battery)
//	self rate    self use / yield
//	consumption  import + yield − export + discharge − charge
//	autarky      (consumption − import) / consumption
//	forecast     expected yield (Forecast.Solar, Solcast in Home Assistant)

import (
	"time"

	"andon/internal/sources"
)

// fiveMinutesPerHour turns a 5-minute step's kWh into kW.
const fiveMinutesPerHour = 12

// SolarSpan is a span's energy in kWh.
type SolarSpan struct {
	Yield, Import, Export, Charge, Discharge float64
}

// SolarSum adds days (or months) up.
func SolarSum(days []sources.SolarDay) SolarSpan {
	var s SolarSpan
	for _, d := range days {
		s.Yield += d.Solar
		s.Import += d.Import
		s.Export += d.Export
		s.Charge += d.Charge
		s.Discharge += d.Discharge
	}
	return s
}

// SelfUse is the yield not exported.
func (s SolarSpan) SelfUse() float64 { return max(s.Yield-s.Export, 0) }

// Consumption is what the house used, from the grid, the panels and the
// battery.
func (s SolarSpan) Consumption() float64 {
	return max(s.Import+s.Yield-s.Export+s.Discharge-s.Charge, 0)
}

// SelfRate is the share of the yield used at home; false without yield.
func (s SolarSpan) SelfRate() (float64, bool) {
	if s.Yield <= 0 {
		return 0, false
	}
	return s.SelfUse() / s.Yield, true
}

// Autarky is the share of the consumption not imported; false without
// consumption or without a known grid (no import nor export).
func (s SolarSpan) Autarky() (float64, bool) {
	used := s.Consumption()
	if used <= 0 || s.Import+s.Export == 0 {
		return 0, false
	}
	return min(max((used-s.Import)/used, 0), 1), true
}

// SolarOn is one day's figures ("2026-10-28"); false when the
// statistics have none.
func SolarOn(e *sources.HassEnergy, day string) (sources.SolarDay, bool) {
	if e == nil {
		return sources.SolarDay{}, false
	}
	for i := len(e.Days) - 1; i >= 0; i-- {
		if e.Days[i].Day == day {
			return e.Days[i], true
		}
	}
	return sources.SolarDay{}, false
}

// SolarMonth is one month's figures ("2026-10").
func SolarMonth(e *sources.HassEnergy, month string) (sources.SolarDay, bool) {
	if e == nil {
		return sources.SolarDay{}, false
	}
	for _, m := range e.Months {
		if m.Day == month {
			return m, true
		}
	}
	return sources.SolarDay{}, false
}

// SolarDayKey is day's key in Home Assistant's zone: "2026-10-28".
func SolarDayKey(e *sources.HassEnergy, t time.Time) string {
	return t.In(e.Location()).Format(time.DateOnly)
}

// ForecastOn is the expected yield of a day; false without forecast
// hours on that day.
func ForecastOn(e *sources.HassEnergy, day string) (float64, bool) {
	if e == nil {
		return 0, false
	}
	loc := e.Location()
	sum, found := 0.0, false
	for _, p := range e.Forecast {
		if p.At.In(loc).Format(time.DateOnly) == day {
			sum += p.KWh
			found = true
		}
	}
	return sum, found
}

// SolarPower is the solar power of the last 5 minutes in kW and when
// that step began; false without a curve.
func SolarPower(e *sources.HassEnergy) (float64, time.Time, bool) {
	if e == nil || len(e.Today) == 0 {
		return 0, time.Time{}, false
	}
	last := e.Today[len(e.Today)-1]
	return last.KWh * fiveMinutesPerHour, last.At, true
}

// SolarHours adds the 5-minute curve up per hour, oldest first.
func SolarHours(e *sources.HassEnergy) []sources.SolarPoint {
	var out []sources.SolarPoint
	for _, p := range e.Today {
		hour := p.At.Truncate(time.Hour)
		if n := len(out); n > 0 && out[n-1].At.Equal(hour) {
			out[n-1].KWh += p.KWh
			continue
		}
		out = append(out, sources.SolarPoint{At: hour, KWh: p.KWh})
	}
	return out
}

// BatteryIdleDays counts the full days before today since the battery
// last charged and discharged at least minKWh each; false without a
// battery or without such a day in the statistics.
func BatteryIdleDays(e *sources.HassEnergy, today string, minKWh float64) (int, bool) {
	if !e.Battery() {
		return 0, false
	}
	idle := 0
	for i := len(e.Days) - 1; i >= 0; i-- {
		d := e.Days[i]
		if d.Day >= today {
			continue
		}
		if d.Charge >= minKWh && d.Discharge >= minKWh {
			return idle, true
		}
		idle++
	}
	return 0, false
}

// Sample keys of the PV figures: one value per day.
var (
	SolarYieldKey    = key("solar", "yield")
	SolarSelfKey     = key("solar", "selfrate")
	SolarAutarkyKey  = key("solar", "autarky")
	SolarForecastKey = key("solar", "forecast")
)

func init() {
	// Yesterday's figures, complete once the day is over; today's
	// forecast, kept as the day's last run saw it (yield against it).
	Record(func(e *sources.HassEnergy, now time.Time, r *Readings) {
		if !e.Configured() {
			return
		}
		loc := e.Location()
		yesterday := now.In(loc).AddDate(0, 0, -1).Format(time.DateOnly)
		if d, ok := SolarOn(e, yesterday); ok {
			span := SolarSum([]sources.SolarDay{d})
			r.SetOn(yesterday, SolarYieldKey, span.Yield)
			if rate, ok := span.SelfRate(); ok {
				r.SetOn(yesterday, SolarSelfKey, rate)
			}
			if rate, ok := span.Autarky(); ok {
				r.SetOn(yesterday, SolarAutarkyKey, rate)
			}
		}
		if f, ok := ForecastOn(e, now.In(loc).Format(time.DateOnly)); ok {
			r.Set(SolarForecastKey, f)
		}
	})
}
