package sources

// Home Assistant's energy figures: PV yield, grid import and export,
// battery charge and discharge, as its energy dashboard knows them.
// Home Assistant collects (long-term statistics), Andon reads them:
//
//	get_config                        → its time zone (days are its days)
//	energy/get_prefs                  → which statistic is solar, grid, battery
//	  (none: the connection's options energy_solar, energy_import …)
//	recorder/statistics_during_period → per day (400 days), per month (25),
//	                                    solar per 5 minutes today
//	energy/solar_forecast             → expected yield per hour, if set up
//
// Any PV counts (SMA, Fronius, OpenDTU …): Andon knows no inverter.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// HassEnergyKey is the source's key, also its dataset's key in the
// analysis (a companion of the Home Assistant dataset).
const HassEnergyKey = "homeassistant.energy"

// Spans read from the statistics.
const (
	energyDaysBack   = 400 // a year and a bit: the same days last year
	energyMonthsBack = 25  // this month and two years
)

// Statistic periods and units of recorder/statistics_during_period.
const (
	periodDay     = "day"
	periodMonth   = "month"
	period5Minute = "5minute"
	unitKWh       = "kWh"
	whPerKWh      = 1000
)

// Source types of energy/get_prefs.
const (
	prefGrid    = "grid"
	prefSolar   = "solar"
	prefBattery = "battery"
)

// Connection options naming the statistics when Home Assistant has no
// energy dashboard: ids separated by commas.
const (
	OptEnergySolar  = "energy_solar"
	OptEnergyImport = "energy_import"
	OptEnergyExport = "energy_export"
)

// SolarDay is one day ("2026-10-28") or month ("2026-10") in kWh.
type SolarDay struct {
	Day                                      string
	Solar, Import, Export, Charge, Discharge float64
}

// SolarPoint is one step of a curve: the kWh from At on.
type SolarPoint struct {
	At  time.Time
	KWh float64
}

// HassEnergy is one Home Assistant's energy figures. Without a solar
// statistic it is empty: Solar is nil, the rest too.
type HassEnergy struct {
	URL  string
	Zone string // Home Assistant's time zone, its days' and months' zone

	// The statistics per role.
	Solar, Import, Export, Charge, Discharge []string

	Days     []SolarDay   // oldest first, today (so far) last
	Months   []SolarDay   // oldest first, this month (so far) last
	Today    []SolarPoint // solar per 5 minutes, oldest first
	Forecast []SolarPoint // expected solar per hour, today and on; nil without forecast
}

// Configured says whether a solar statistic is known.
func (e *HassEnergy) Configured() bool { return e != nil && len(e.Solar) > 0 }

// Battery says whether a battery is known.
func (e *HassEnergy) Battery() bool { return e != nil && len(e.Charge)+len(e.Discharge) > 0 }

// Location is the zone the days are in; the server's when unknown.
func (e *HassEnergy) Location() *time.Location {
	if e == nil {
		return time.Local
	}
	if loc, err := time.LoadLocation(e.Zone); err == nil && e.Zone != "" {
		return loc
	}
	return time.Local
}

// energyTTL: the statistics are hourly; the curve of today moves every
// five minutes.
const energyTTL = 15 * time.Minute

var HassEnergyData = source{key: HassEnergyKey, ttl: energyTTL, service: enums.ServiceHomeAssistant, fetch: fetchHassEnergy}

func fetchHassEnergy(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		data := DemoHassEnergy(time.Now())
		data.URL = sctx.URL
		return data, nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HassApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	s, err := api.Open(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	defer s.Close()
	return readEnergy(ctx, s, sctx, time.Now())
}

// commander runs one WebSocket command (services.HassSession; a fake in
// tests).
type commander interface {
	Command(ctx context.Context, kind string, params map[string]any) (any, error)
}

func readEnergy(ctx context.Context, s commander, sctx Ctx, now time.Time) (*HassEnergy, error) {
	out := &HassEnergy{URL: sctx.URL}
	cfg, err := s.Command(ctx, "get_config", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	out.Zone = asStr(asMap(cfg)["time_zone"])

	// Without the energy integration (or its setup) the command is
	// unknown: the options name the statistics then. A broken connection
	// fails the fetch: cached as empty it would read as "no PV".
	prefs, err := s.Command(ctx, "energy/get_prefs", nil)
	if err != nil && !errors.As(err, new(services.ApiMissing)) {
		return nil, fetchError(err)
	}
	forecasts := out.setRoles(prefs)
	// The options fill only the roles the dashboard leaves empty.
	for _, role := range []struct {
		ids *[]string
		opt string
	}{{&out.Solar, OptEnergySolar}, {&out.Import, OptEnergyImport}, {&out.Export, OptEnergyExport}} {
		if len(*role.ids) == 0 {
			*role.ids = optionIDs(sctx.Options, role.opt)
		}
	}
	if !out.Configured() {
		return out, nil
	}

	loc := out.Location()
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	all := slices.Concat(out.Solar, out.Import, out.Export, out.Charge, out.Discharge)

	days, err := statistics(ctx, s, all, periodDay, today.AddDate(0, 0, -energyDaysBack), now)
	if err != nil {
		return nil, fetchError(err)
	}
	out.Days = out.sums(days, loc, time.DateOnly)
	months, err := statistics(ctx, s, all, periodMonth, time.Date(local.Year(), local.Month()-energyMonthsBack+1, 1, 0, 0, 0, 0, loc), now)
	if err != nil {
		return nil, fetchError(err)
	}
	out.Months = out.sums(months, loc, monthLayout)
	if curve, err := statistics(ctx, s, out.Solar, period5Minute, today, now); err == nil {
		out.Today = curveOf(curve, out.Solar)
	}

	// The forecast is a bonus: a failing one leaves it out.
	if len(forecasts) > 0 {
		if raw, err := s.Command(ctx, "energy/solar_forecast", nil); err == nil {
			out.Forecast = forecastOf(raw, forecasts)
		}
	}
	return out, nil
}

// monthLayout keys a month: "2026-10".
const monthLayout = "2006-01"

// setRoles reads energy/get_prefs and returns the forecast entries of
// its solar sources. Grid sources come as flow_from/flow_to lists or,
// in newer versions, with the statistics on the source itself.
//
//	{"energy_sources": [
//	  {"type": "grid", "flow_from": [{"stat_energy_from": "sensor.import"}], "flow_to": [{"stat_energy_to": "sensor.export"}]},
//	  {"type": "solar", "stat_energy_from": "sensor.pv", "config_entry_solar_forecast": ["01J…"]},
//	  {"type": "battery", "stat_energy_from": "sensor.out", "stat_energy_to": "sensor.in"}]}
func (e *HassEnergy) setRoles(prefs any) []string {
	var forecasts []string
	add := func(list *[]string, id string) {
		if id != "" && !slices.Contains(*list, id) {
			*list = append(*list, id)
		}
	}
	for _, raw := range asList(asMap(prefs)["energy_sources"]) {
		src := asMap(raw)
		from, to := asStr(src["stat_energy_from"]), asStr(src["stat_energy_to"])
		switch asStr(src["type"]) {
		case prefSolar:
			add(&e.Solar, from)
			for _, f := range asList(src["config_entry_solar_forecast"]) {
				add(&forecasts, asStr(f))
			}
		case prefBattery:
			add(&e.Discharge, from)
			add(&e.Charge, to)
		case prefGrid:
			add(&e.Import, from)
			add(&e.Export, to)
			for _, f := range asList(src["flow_from"]) {
				add(&e.Import, asStr(asMap(f)["stat_energy_from"]))
			}
			for _, f := range asList(src["flow_to"]) {
				add(&e.Export, asStr(asMap(f)["stat_energy_to"]))
			}
		}
	}
	return forecasts
}

// optionIDs reads a comma-separated option: "sensor.a, sensor.b".
func optionIDs(options map[string]any, key string) []string {
	var out []string
	for _, id := range strings.Split(asStr(options[key]), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// statistics reads the change per period of ids from start to end, in
// kWh: {"sensor.pv": [{"start": 1761602400000, "change": 12.3}, …]}.
func statistics(ctx context.Context, s commander, ids []string, period string, start, end time.Time) (map[string][]SolarPoint, error) {
	raw, err := s.Command(ctx, "recorder/statistics_during_period", map[string]any{
		"start_time": start.UTC().Format(time.RFC3339), "end_time": end.UTC().Format(time.RFC3339),
		"statistic_ids": ids, "period": period, "types": []string{"change"}, "units": map[string]string{"energy": unitKWh},
	})
	if err != nil {
		return nil, err
	}
	out := map[string][]SolarPoint{}
	for id, rows := range asMap(raw) {
		for _, row := range asList(rows) {
			m := asMap(row)
			out[id] = append(out[id], SolarPoint{At: statTime(m["start"]), KWh: asFloat(m["change"])})
		}
	}
	return out, nil
}

// statTime reads a statistic's start: milliseconds since the epoch
// (2023.3 on) or an ISO time (before).
func statTime(v any) time.Time {
	if ms, ok := v.(float64); ok {
		return time.UnixMilli(int64(ms))
	}
	return parseTime(v)
}

// sums adds up each role's statistics per period, keyed by layout in
// the zone: oldest first.
func (e *HassEnergy) sums(stats map[string][]SolarPoint, loc *time.Location, layout string) []SolarDay {
	byDay := map[string]*SolarDay{}
	add := func(ids []string, field func(*SolarDay) *float64) {
		for _, id := range ids {
			for _, p := range stats[id] {
				day := p.At.In(loc).Format(layout)
				if byDay[day] == nil {
					byDay[day] = &SolarDay{Day: day}
				}
				*field(byDay[day]) += p.KWh
			}
		}
	}
	add(e.Solar, func(d *SolarDay) *float64 { return &d.Solar })
	add(e.Import, func(d *SolarDay) *float64 { return &d.Import })
	add(e.Export, func(d *SolarDay) *float64 { return &d.Export })
	add(e.Charge, func(d *SolarDay) *float64 { return &d.Charge })
	add(e.Discharge, func(d *SolarDay) *float64 { return &d.Discharge })

	out := make([]SolarDay, 0, len(byDay))
	for _, day := range slices.Sorted(maps.Keys(byDay)) {
		out = append(out, *byDay[day])
	}
	return out
}

// curveOf adds the solar statistics up per step, oldest first.
func curveOf(stats map[string][]SolarPoint, ids []string) []SolarPoint {
	byAt := map[time.Time]float64{}
	for _, id := range ids {
		for _, p := range stats[id] {
			byAt[p.At] += p.KWh
		}
	}
	return pointsOf(byAt)
}

// forecastOf adds up the forecast entries' hours, Wh to kWh:
//
//	{"01J…": {"wh_hours": {"2026-10-29T10:00:00+01:00": 812, …}}}
func forecastOf(raw any, entries []string) []SolarPoint {
	byAt := map[time.Time]float64{}
	all := asMap(raw)
	for _, entry := range entries {
		for at, wh := range asMap(asMap(all[entry])["wh_hours"]) {
			t := parseTime(at)
			if t.IsZero() {
				continue
			}
			byAt[t] += asFloat(wh) / whPerKWh
		}
	}
	if len(byAt) == 0 {
		return nil
	}
	return pointsOf(byAt)
}

func pointsOf(byAt map[time.Time]float64) []SolarPoint {
	out := make([]SolarPoint, 0, len(byAt))
	for at, kwh := range byAt {
		out = append(out, SolarPoint{At: at, KWh: kwh})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func init() {
	Register(HassEnergyData)
	Companion(enums.ServiceHomeAssistant, HassEnergyKey)
}
