package rules

// PV from Home Assistant's energy statistics (its companion dataset
// sources.HassEnergyKey):
//
//	solar.yield_low      yesterday made under "share" of its forecast (or,
//	                     without one, of the same days last year)
//	solar.no_yield       today "hours" or more with forecast but no yield
//	solar.battery_idle   the battery has not cycled for "days"
//	solar.selfuse_drop   this month's self-use rate is "points" below the
//	                     same month last year
//	cross.solar_selfuse  this month's export at the feed-in tariff while
//	                     Tibber's price is higher: what self use would save

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

func init() {
	Register("solar.yield_low", hassSvc, map[string]any{"share": 0.7, "min_kwh": 1.0}, on(solarYieldLow))
	Register("solar.no_yield", hassSvc, map[string]any{"hours": 2.0, "kwh": 0.1}, on(solarNoYield))
	Register("solar.battery_idle", hassSvc, map[string]any{"days": 3.0, "kwh": 0.2}, on(solarBatteryIdle))
	Register("solar.selfuse_drop", hassSvc, map[string]any{"points": 10.0, "days": 7.0}, on(solarSelfDrop))
	Register("cross.solar_selfuse", Cross, map[string]any{"feed_in": 0.08, "kwh": 20.0}, solarSelfuse)
	Needs("cross.solar_selfuse", hassSvc, tibberSvc)
}

// energyOf is the scope's PV statistics; false without a solar one.
func energyOf(env Env) (*sources.HassEnergy, bool) {
	e, ok := env.Datasets[sources.HassEnergyKey].(*sources.HassEnergy)
	return e, ok && e.Configured()
}

// solarFinding links Home Assistant's energy dashboard.
func solarFinding(e *sources.HassEnergy, rule string, level enums.Severity, params map[string]any) Finding {
	url := strings.TrimRight(e.URL, "/") + "/energy"
	return svcFinding(hassSvc, rule, rule, rule, level, url, params)
}

func solarYieldLow(_ *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	e, ok := energyOf(env)
	if !ok {
		return nil
	}
	yesterday := metrics.SolarDayKey(e, env.Today.AddDate(0, 0, -1))
	d, ok := metrics.SolarOn(e, yesterday)
	if !ok {
		return nil
	}
	h, _ := env.Datasets[metrics.HistoryDataset].(*metrics.History)
	expected, basis := metrics.ExpectedYield(e, h, yesterday)
	if basis == metrics.ExpectNone || expected < cfgFloat(cfg, "min_kwh") || d.Solar >= expected*cfgFloat(cfg, "share") {
		return nil
	}

	msg := "solar.yield_low"
	if basis == metrics.ExpectLastYear {
		msg = "solar.yield_low_year"
	}
	f := solarFinding(e, "solar.yield_low", enums.SeverityWarn, map[string]any{"day": DayStr(yesterday),
		"kwh": Num(d.Solar, 1), "expected": Num(expected, 1), "pct": Num(d.Solar/expected*100, 0)})
	f.Message = msg
	return []Finding{f}
}

func solarNoYield(_ *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	e, ok := energyOf(env)
	if !ok {
		return nil
	}
	n := metrics.DarkHours(e, env.Today, cfgFloat(cfg, "kwh"))
	if n < int(cfgFloat(cfg, "hours")) {
		return nil
	}
	return []Finding{solarFinding(e, "solar.no_yield", enums.SeverityCritical, map[string]any{"hours": n})}
}

func solarBatteryIdle(_ *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	e, ok := energyOf(env)
	if !ok {
		return nil
	}
	n, ok := metrics.BatteryIdleDays(e, metrics.SolarDayKey(e, env.Today), cfgFloat(cfg, "kwh"))
	if !ok || n < int(cfgFloat(cfg, "days")) {
		return nil
	}
	return []Finding{solarFinding(e, "solar.battery_idle", enums.SeverityWarn, map[string]any{"days": n})}
}

func solarSelfDrop(_ *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	e, ok := energyOf(env)
	if !ok || env.Today.In(e.Location()).Day() <= int(cfgFloat(cfg, "days")) {
		return nil // too few days for a month's rate
	}
	this, before, ok := metrics.SelfRateYearAgo(e, env.Today)
	if !ok || (before-this)*100 < cfgFloat(cfg, "points") {
		return nil
	}
	return []Finding{solarFinding(e, "solar.selfuse_drop", enums.SeverityInfo, map[string]any{
		"pct": Num(this*100, 0), "before": Num(before*100, 0)})}
}

func solarSelfuse(_ any, cfg map[string]any, env Env) []Finding {
	e, ok := energyOf(env)
	tib, ok2 := env.Datasets[tibberSvc].(*sources.TibberDataset)
	if !ok || !ok2 || len(tib.Prices) == 0 || len(e.Export) == 0 {
		return nil
	}
	month, ok := metrics.SolarMonthAt(e, env.Today)
	feedIn := cfgFloat(cfg, "feed_in")
	price := metrics.PowerPrice(tib, 0)
	if !ok || month.Export < cfgFloat(cfg, "kwh") || price <= feedIn {
		return nil
	}

	f := solarFinding(e, "cross.solar_selfuse", enums.SeverityInfo, map[string]any{"kwh": Num(month.Export, 0),
		"price": Num(price*100, 0), "feed_in": Num(feedIn*100, 0), "saving": Money(month.Export*(price-feedIn), tib.Currency)})
	f.Sources = []string{hassSvc, tibberSvc}
	return []Finding{f}
}
