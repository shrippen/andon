package metrics_test

import (
	"math"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// 10 kWh made, 2 exported, 5 imported, battery in 3 out 2.5:
// self use 8 (80 %), consumption 12.5, autarky 7.5/12.5 = 60 %.
func TestSolarSpan(t *testing.T) {
	grid := &sources.HassEnergy{Solar: []string{"pv"}, Import: []string{"in"}, Export: []string{"out"}}
	s := metrics.SolarSum(grid, []sources.SolarDay{{Solar: 4}, {Solar: 6, Import: 5, Export: 2, Charge: 3, Discharge: 2.5}})
	rate, ok := s.SelfRate()
	aut, ok2 := s.Autarky()
	if s.Yield != 10 || s.SelfUse() != 8 || !ok || !near(rate, 0.8) || s.Consumption() != 12.5 || !ok2 || !near(aut, 0.6) {
		t.Fatalf("span %+v rate %v aut %v", s, rate, aut)
	}
	if _, ok := metrics.SolarSum(grid, nil).SelfRate(); ok {
		t.Fatal("rate without yield")
	}
	if _, ok := metrics.SolarSum(nil, []sources.SolarDay{{Solar: 3}}).Autarky(); ok {
		t.Fatal("autarky without a grid")
	}
	// 25 October: 02:00–03:00 comes twice in Berlin, as two hours.
	dst := &sources.HassEnergy{Zone: "Europe/Berlin", Today: []sources.SolarPoint{
		{At: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), KWh: 1}, {At: time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), KWh: 2}}}
	if h := metrics.SolarHours(dst); len(h) != 2 || h[1].At.Sub(h[0].At) != time.Hour {
		t.Fatalf("clock change: %+v", h)
	}
	if metrics.SolarHours(nil) != nil || metrics.SolarDayKey(nil, time.Now()) == "" {
		t.Fatal("nil dataset")
	}
}

func solarFixture() *sources.HassEnergy {
	berlin := "Europe/Berlin"
	loc, _ := time.LoadLocation(berlin)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 29, h, m, 0, 0, loc) }
	return &sources.HassEnergy{Zone: berlin, Solar: []string{"pv"}, Charge: []string{"in"}, Discharge: []string{"out"},
		Days: []sources.SolarDay{
			{Day: "2026-10-25", Solar: 9, Charge: 2, Discharge: 2},
			{Day: "2026-10-26", Solar: 9, Charge: 2},
			{Day: "2026-10-27", Solar: 9},
			{Day: "2026-10-28", Solar: 8},
			{Day: "2026-10-29", Solar: 1, Charge: 1, Discharge: 1},
		},
		Today:    []sources.SolarPoint{{At: at(10, 0), KWh: 0.1}, {At: at(10, 5), KWh: 0.2}, {At: at(11, 0), KWh: 0.25}},
		Forecast: []sources.SolarPoint{{At: at(10, 0), KWh: 1}, {At: at(11, 0), KWh: 2}, {At: at(10, 0).AddDate(0, 0, 1), KWh: 4}},
	}
}

func TestSolarFigures(t *testing.T) {
	e := solarFixture()
	if d, ok := metrics.SolarOn(e, "2026-10-28"); !ok || d.Solar != 8 {
		t.Fatalf("day: %+v", d)
	}
	if f, ok := metrics.ForecastOn(e, "2026-10-29"); !ok || f != 3 {
		t.Fatalf("forecast: %v", f)
	}
	if _, ok := metrics.ForecastOn(e, "2026-10-28"); ok {
		t.Fatal("forecast for a day without hours")
	}
	if kw, _, ok := metrics.SolarPower(e); !ok || kw != 3 {
		t.Fatalf("power: %v", kw)
	}
	if h := metrics.SolarHours(e); len(h) != 2 || !near(h[0].KWh, 0.3) || h[1].KWh != 0.25 {
		t.Fatalf("hours: %+v", h)
	}

	// The 25th was the last full cycle; today's does not count.
	if n, ok := metrics.BatteryIdleDays(e, "2026-10-29", 0.5); !ok || n != 3 {
		t.Fatalf("idle: %v %v", n, ok)
	}
	e.Charge, e.Discharge = nil, nil
	if _, ok := metrics.BatteryIdleDays(e, "2026-10-29", 0.5); ok {
		t.Fatal("idle without a battery")
	}
}

// Rate and autarky need their roles, not values: without an export
// statistic there is no rate, without import and export no autarky; a
// day without grid flow is fully self-sufficient.
func TestSolarRoles(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	read := func(e *sources.HassEnergy) map[string]float64 {
		e.Days = []sources.SolarDay{{Day: "2026-10-28", Solar: 5, Discharge: 1, Charge: 1}}
		return metrics.Read(metrics.Scope{Datasets: map[string]any{sources.HassEnergyKey: e}}, now).Past["2026-10-28"]
	}

	only := read(&sources.HassEnergy{Solar: []string{"pv"}})
	if _, ok := only[metrics.SolarSelfKey]; ok {
		t.Fatalf("rate without export: %v", only)
	}
	noImport := read(&sources.HassEnergy{Solar: []string{"pv"}, Export: []string{"out"}})
	if _, ok := noImport[metrics.SolarAutarkyKey]; ok {
		t.Fatalf("autarky without import: %v", noImport)
	}
	grid := read(&sources.HassEnergy{Solar: []string{"pv"}, Import: []string{"in"}, Export: []string{"out"}})
	if grid[metrics.SolarAutarkyKey] != 1 || grid[metrics.SolarSelfKey] != 1 {
		t.Fatalf("day off the grid: %v", grid)
	}
}

// The analysis keeps yesterday's figures on yesterday and today's
// forecast as today's value.
func TestSolarRecorded(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r := metrics.Read(metrics.Scope{Datasets: map[string]any{sources.HassEnergyKey: solarFixture()}}, now)
	if r.Past["2026-10-28"][metrics.SolarYieldKey] != 8 || r.Past["2026-10-29"][metrics.SolarForecastKey] != 3 {
		t.Fatalf("readings: %+v %+v", r.Past, r.Values)
	}

	// 00:30 in Berlin is still the 28th in UTC: the forecast belongs to
	// Home Assistant's 29th, not to the 28th it would overwrite.
	r = metrics.Read(metrics.Scope{Datasets: map[string]any{sources.HassEnergyKey: solarFixture()}}, time.Date(2026, 10, 28, 23, 30, 0, 0, time.UTC))
	if _, ok := r.Values[metrics.SolarForecastKey]; ok || r.Past["2026-10-29"][metrics.SolarForecastKey] != 3 {
		t.Fatalf("after midnight: %+v %+v", r.Past, r.Values)
	}
}

// The running hour is not dark yet: at 10:05 the curve's one 5-minute
// point of hour 10 says nothing about the rest of it.
func TestDarkHoursRunning(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	at := func(h, m int) time.Time { return time.Date(2026, 7, 15, h, m, 0, 0, loc) }
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"},
		Forecast: []sources.SolarPoint{{At: at(8, 0), KWh: 0.5}, {At: at(9, 0), KWh: 1}, {At: at(10, 0), KWh: 1.5}}}
	for h := 8; h <= 9; h++ {
		for m := 0; m < 60; m += 5 {
			kwh := 0.0
			if h == 8 {
				kwh = 0.05
			}
			e.Today = append(e.Today, sources.SolarPoint{At: at(h, m), KWh: kwh})
		}
	}
	e.Today = append(e.Today, sources.SolarPoint{At: at(10, 0)})

	if n := metrics.DarkHours(e, at(10, 5), 0.1); n != 1 {
		t.Fatalf("dark hours at 10:05: %d, want 1 (only 9:00–10:00)", n)
	}
}
