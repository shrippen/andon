package sources

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"andon/internal/drivers/services"
)

// fakeHass answers WebSocket commands from recorded results; a command
// without one fails, as Home Assistant does without the energy
// integration.
type fakeHass struct {
	answers map[string]string // command → result JSON
	asked   []map[string]any  // statistics requests, in order
	down    string            // the connection breaks at this command
}

func (f *fakeHass) Command(_ context.Context, kind string, params map[string]any) (any, error) {
	if kind == f.down {
		return nil, errors.New("read: connection reset")
	}
	if kind == "recorder/statistics_during_period" {
		f.asked = append(f.asked, params)
		kind += "/" + params["period"].(string)
	}
	raw, ok := f.answers[kind]
	if !ok {
		return nil, services.ApiMissing{} // as HassSession for an unknown command
	}
	var out any
	return out, json.Unmarshal([]byte(raw), &out)
}

// Berlin midnight 28.10.2026 (CET) and 27.10.2026, in milliseconds.
const (
	ms28 = "1793142000000"
	ms27 = "1793055600000"
)

func hassEnergyFake() *fakeHass {
	return &fakeHass{answers: map[string]string{
		"get_config": `{"time_zone": "Europe/Berlin"}`,
		"energy/get_prefs": `{"energy_sources": [
			{"type": "grid", "flow_from": [{"stat_energy_from": "sensor.import"}], "flow_to": [{"stat_energy_to": "sensor.export"}]},
			{"type": "solar", "stat_energy_from": "sensor.pv_east", "config_entry_solar_forecast": ["fc1"]},
			{"type": "solar", "stat_energy_from": "sensor.pv_west", "config_entry_solar_forecast": ["fc2"]},
			{"type": "battery", "stat_energy_from": "sensor.bat_out", "stat_energy_to": "sensor.bat_in"},
			{"type": "gas", "stat_energy_from": "sensor.gas"}]}`,
		"recorder/statistics_during_period/day": `{
			"sensor.pv_east": [{"start": ` + ms27 + `, "change": 4.0}, {"start": ` + ms28 + `, "change": 6.5}],
			"sensor.pv_west": [{"start": ` + ms28 + `, "change": 3.5}],
			"sensor.import":  [{"start": ` + ms28 + `, "change": 5.0}],
			"sensor.export":  [{"start": ` + ms28 + `, "change": 2.0}],
			"sensor.bat_in":  [{"start": ` + ms28 + `, "change": 3.0}],
			"sensor.bat_out": [{"start": ` + ms28 + `, "change": 2.5}]}`,
		"recorder/statistics_during_period/month": `{"sensor.pv_east": [{"start": "2026-09-30T22:00:00Z", "change": 180}]}`,
		"recorder/statistics_during_period/5minute": `{
			"sensor.pv_east": [{"start": 1793264400000, "change": 0.2}, {"start": 1793264700000, "change": 0.25}],
			"sensor.pv_west": [{"start": 1793264700000, "change": 0.05}]}`,
		"energy/solar_forecast": `{
			"fc1": {"wh_hours": {"2026-10-29T10:00:00+01:00": 750, "2026-10-29T11:00:00+01:00": 1200}},
			"fc2": {"wh_hours": {"2026-10-29T10:00:00+01:00": 250}},
			"other": {"wh_hours": {"2026-10-29T10:00:00+01:00": 9999}}}`,
	}}
}

// The energy dashboard names the statistics; days are Home Assistant's
// days, several panels add up, gas and foreign forecasts stay out.
func TestReadEnergy(t *testing.T) {
	f := hassEnergyFake()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	e, err := readEnergy(context.Background(), f, Ctx{URL: "http://ha"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.Zone != "Europe/Berlin" || len(e.Solar) != 2 || e.Import[0] != "sensor.import" || e.Export[0] != "sensor.export" ||
		e.Charge[0] != "sensor.bat_in" || e.Discharge[0] != "sensor.bat_out" || !e.Battery() {
		t.Fatalf("roles: %+v", e)
	}
	want := []SolarDay{{Day: "2026-10-27", Solar: 4}, {Day: "2026-10-28", Solar: 10, Import: 5, Export: 2, Charge: 3, Discharge: 2.5}}
	if len(e.Days) != 2 || e.Days[0] != want[0] || e.Days[1] != want[1] {
		t.Fatalf("days: %+v", e.Days)
	}
	if len(e.Months) != 1 || e.Months[0].Day != "2026-10" || e.Months[0].Solar != 180 {
		t.Fatalf("months: %+v", e.Months)
	}
	if len(e.Today) != 2 || e.Today[1].KWh != 0.3 {
		t.Fatalf("today: %+v", e.Today)
	}
	if len(e.Forecast) != 2 || e.Forecast[0].KWh != 1 || e.Forecast[1].KWh != 1.2 {
		t.Fatalf("forecast: %+v", e.Forecast)
	}

	// Days from midnight in Home Assistant's zone, 400 back.
	first := f.asked[0]
	if first["start_time"] != "2025-09-23T22:00:00Z" || first["period"] != "day" {
		t.Fatalf("day request: %v", first)
	}
}

// Without the energy integration the options name the statistics;
// without any, the dataset is empty and asks nothing more.
func TestReadEnergyOptions(t *testing.T) {
	f := hassEnergyFake()
	delete(f.answers, "energy/get_prefs")
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	e, err := readEnergy(context.Background(), f, Ctx{Options: map[string]any{OptEnergySolar: "sensor.pv_east, sensor.pv_west", OptEnergyImport: "sensor.import"}}, now)
	if err != nil || len(e.Solar) != 2 || len(e.Import) != 1 || e.Battery() || e.Forecast != nil {
		t.Fatalf("options: %+v %v", e, err)
	}

	// A dashboard with the grid but no solar keeps its grid; the
	// options add only the solar.
	f.answers["energy/get_prefs"] = `{"energy_sources": [{"type": "grid", "stat_energy_from": "sensor.in", "stat_energy_to": "sensor.out"}]}`
	e, err = readEnergy(context.Background(), f, Ctx{Options: map[string]any{OptEnergySolar: "sensor.pv_east"}}, now)
	if err != nil || len(e.Solar) != 1 || len(e.Import) != 1 || e.Import[0] != "sensor.in" || len(e.Export) != 1 {
		t.Fatalf("grid from the dashboard: %+v %v", e, err)
	}
	delete(f.answers, "energy/get_prefs")

	// A broken connection is a failed fetch, not "no PV".
	f.down = "energy/get_prefs"
	if _, err := readEnergy(context.Background(), f, Ctx{}, now); err == nil {
		t.Fatal("connection lost read as no PV")
	}
	f.down = ""

	f.asked = nil
	e, err = readEnergy(context.Background(), f, Ctx{}, now)
	if err != nil || e.Configured() || len(f.asked) != 0 {
		t.Fatalf("none: %+v %v %v", e, err, f.asked)
	}
}

// Newer versions keep the grid's statistics on the source itself.
func TestEnergyRolesFlatGrid(t *testing.T) {
	var prefs any
	_ = json.Unmarshal([]byte(`{"energy_sources": [{"type": "grid", "stat_energy_from": "sensor.in", "stat_energy_to": "sensor.out"}]}`), &prefs)
	var e HassEnergy
	e.setRoles(prefs)
	if len(e.Import) != 1 || e.Import[0] != "sensor.in" || len(e.Export) != 1 || e.Export[0] != "sensor.out" {
		t.Fatalf("grid: %+v", e)
	}
}
