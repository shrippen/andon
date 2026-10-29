package sources_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// The demo's PV tells its story: yesterday half the forecast (garage
// down), the battery idle for days, self use below last year's.
func TestDemoHassEnergy(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	e := sources.DemoHassEnergy(now)
	if !e.Configured() || !e.Battery() || len(e.Months) != 25 || len(e.Days) != 401 {
		t.Fatalf("shape: %d months %d days", len(e.Months), len(e.Days))
	}

	today := metrics.SolarDayKey(e, now)
	tomorrow := metrics.SolarDayKey(e, now.AddDate(0, 0, 1))
	if _, ok := metrics.ForecastOn(e, tomorrow); !ok {
		t.Fatal("no forecast for tomorrow")
	}
	fc, _ := metrics.ForecastOn(e, today)
	sofar, _ := metrics.SolarOn(e, today)
	if kw, _, ok := metrics.SolarPower(e); !ok || kw <= 0 || sofar.Solar <= 0 || sofar.Solar >= fc/2 {
		t.Fatalf("today: %v kW, %v of %v kWh", kw, sofar.Solar, fc)
	}

	if n, ok := metrics.BatteryIdleDays(e, today, 0.5); !ok || n < 7 {
		t.Fatalf("battery idle %d", n)
	}
	this, _ := metrics.SolarMonth(e, "2026-07")
	last, _ := metrics.SolarMonth(e, "2025-07")
	rate, _ := metrics.SolarSum(e, []sources.SolarDay{this}).SelfRate()
	before, _ := metrics.SolarSum(e, []sources.SolarDay{last}).SelfRate()
	if rate >= before {
		t.Fatalf("self use %v, last year %v", rate, before)
	}
}
