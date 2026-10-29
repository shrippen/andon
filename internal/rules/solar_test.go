package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// 12:00 in Berlin on 15 July 2026.
var solarNow = time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

func solarEnv(e *sources.HassEnergy, h *metrics.History) map[string]any {
	return map[string]any{sources.HassEnergyKey: e, metrics.HistoryDataset: h}
}

// Yesterday against the forecast the analysis recorded; without it,
// against the same days last year.
func TestSolarYieldLow(t *testing.T) {
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"}, Days: []sources.SolarDay{
		{Day: "2025-07-13", Solar: 20}, {Day: "2025-07-14", Solar: 22}, {Day: "2025-07-15", Solar: 18},
		{Day: "2026-07-14", Solar: 9}}}
	env := todayEnv(nil)
	env.Today = solarNow

	env.Datasets = solarEnv(e, nil)
	got := run(t, "solar.yield_low", &sources.HassDataset{}, env)
	if len(got) != 1 || got[0].Message != "solar.yield_low_year" || got[0].Params["pct"].(map[string]any)["$num"] != 45.0 {
		t.Fatalf("last year: %+v", got)
	}

	day := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	env.Datasets = solarEnv(e, &metrics.History{Series: map[string][]metrics.Point{metrics.SolarForecastKey: {{Day: day, Value: 10}}}})
	if got := run(t, "solar.yield_low", &sources.HassDataset{}, env); len(got) != 0 {
		t.Fatalf("90 %% of the forecast: %+v", got)
	}
}

// Two hours with forecast and no yield before now; the hour after the
// curve's end does not count.
func TestSolarNoYield(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	at := func(h int) time.Time { return time.Date(2026, 7, 15, h, 0, 0, 0, loc) }
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"},
		Forecast: []sources.SolarPoint{{At: at(8), KWh: 0.5}, {At: at(9), KWh: 1}, {At: at(10), KWh: 1.5}, {At: at(11), KWh: 2}}}
	// The curve per 5 minutes up to 10:55: yield at 8, none at 9 and 10.
	for h := 8; h <= 10; h++ {
		kwh := 0.0
		if h == 8 {
			kwh = 0.025
		}
		for m := 0; m < 60; m += 5 {
			e.Today = append(e.Today, sources.SolarPoint{At: at(h).Add(time.Duration(m) * time.Minute), KWh: kwh})
		}
	}
	env := todayEnv(nil)
	env.Today, env.Datasets = solarNow, solarEnv(e, nil)
	got := run(t, "solar.no_yield", &sources.HassDataset{}, env)
	if len(got) != 1 || got[0].Params["hours"] != 2 || got[0].Severity != enums.SeverityCritical {
		t.Fatalf("no yield: %+v", got)
	}

	// Yield again at 11: the dark morning is over.
	e.Today = append(e.Today, sources.SolarPoint{At: at(11), KWh: 0.5})
	if got := run(t, "solar.no_yield", &sources.HassDataset{}, env); len(got) != 0 {
		t.Fatalf("recovered: %+v", got)
	}
}

func TestSolarBatteryIdle(t *testing.T) {
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"}, Charge: []string{"in"}, Discharge: []string{"out"},
		Days: []sources.SolarDay{{Day: "2026-07-10", Charge: 2, Discharge: 2}, {Day: "2026-07-11"}, {Day: "2026-07-12"}, {Day: "2026-07-13"}, {Day: "2026-07-14"}}}
	env := todayEnv(nil)
	env.Today, env.Datasets = solarNow, solarEnv(e, nil)
	if got := run(t, "solar.battery_idle", &sources.HassDataset{}, env); len(got) != 1 || got[0].Params["days"] != 4 {
		t.Fatalf("idle: %+v", got)
	}
}

// July: 60 % self use against 80 % last July.
func TestSolarSelfuseDrop(t *testing.T) {
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"}, Export: []string{"out"},
		Months: []sources.SolarDay{{Day: "2025-07", Solar: 100, Export: 20}, {Day: "2026-07", Solar: 50, Export: 20}}}
	env := todayEnv(nil)
	env.Today, env.Datasets = solarNow, solarEnv(e, nil)
	got := run(t, "solar.selfuse_drop", &sources.HassDataset{}, env)
	if len(got) != 1 || got[0].Params["before"].(map[string]any)["$num"] != 80.0 {
		t.Fatalf("drop: %+v", got)
	}

	// On the 5th the month says too little.
	env.Today = time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	if got := run(t, "solar.selfuse_drop", &sources.HassDataset{}, env); len(got) != 0 {
		t.Fatalf("early: %+v", got)
	}
}

// 40 kWh exported at 8 ct while Tibber's mean is 30 ct: 8,80 € saved
// had it been used at home.
func TestSolarSelfuseCross(t *testing.T) {
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"}, Export: []string{"out"},
		Months: []sources.SolarDay{{Day: "2026-07", Solar: 100, Export: 40}}}
	tib := &sources.TibberDataset{Currency: "EUR", Prices: []sources.PricePoint{{Total: 0.25}, {Total: 0.35}}}
	env := todayEnv(nil)
	env.Today, env.Datasets = solarNow, map[string]any{sources.HassEnergyKey: e, "tibber": tib}
	got := run(t, "cross.solar_selfuse", nil, env)
	if len(got) != 1 || got[0].Params["saving"].(map[string]any)["$money"] != 8.8 {
		t.Fatalf("selfuse: %+v", got)
	}
}

// With 5 kWh of forecast left today, the expensive charge names the sun.
func TestChargeExpensivePV(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"},
		Forecast: []sources.SolarPoint{{At: time.Date(2026, 7, 15, 13, 0, 0, 0, loc), KWh: 3}, {At: time.Date(2026, 7, 15, 14, 0, 0, 0, loc), KWh: 2}}}
	env := todayEnv(nil)
	env.Today = solarNow
	env.Datasets = map[string]any{
		"evcc":                &sources.EVCCDataset{Grid: 3000, Loadpoints: []sources.Loadpoint{{Title: "Carport", Charging: true, ChargePower: 3700}}},
		"tibber":              &sources.TibberDataset{Current: 0.40, Prices: []sources.PricePoint{{Total: 0.20}, {Total: 0.40}}},
		sources.HassEnergyKey: e,
	}
	got := run(t, "cross.charge_expensive", nil, env)
	if len(got) != 1 || got[0].Message != "cross.charge_expensive_pv" || got[0].Params["pv"].(map[string]any)["$num"] != 5.0 {
		t.Fatalf("charge: %+v", got)
	}
}
