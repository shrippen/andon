package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// TestUPSRules: on battery is critical, a short runtime and a battery to
// replace are warnings; the rules read every UPS tool.
func TestUPSRules(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"peanut":  &sources.UPSDataset{Tool: enums.ServicePeaNUT, Devices: []sources.UPS{{Name: "nas", OnBattery: true, Runtime: 1200}}},
		"apcupsd": sources.DemoUPS(time.Now(), enums.ServiceApcupsd), // 7 minutes left
	}
	if got := run(t, "ups.on_battery", nil, env); len(got) != 1 || got[0].Severity != enums.SeverityCritical {
		t.Fatalf("on battery: %+v", got)
	}
	if got := run(t, "ups.runtime_low", nil, env); len(got) != 1 || got[0].Params["name"] != "boje-usv" {
		t.Fatalf("runtime: %+v", got)
	}
}

// TestOutageOnBattery: while a UPS runs on battery, the failures of every
// host gather under it as one outage.
func TestOutageOnBattery(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"peanut":     &sources.UPSDataset{Devices: []sources.UPS{{Name: "nas-usv", OnBattery: true}}},
		"uptimekuma": &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "Web", Target: "https://web.lan", Status: sources.KumaDown}}},
	}
	outages := rules.Outages(env)
	if len(outages) != 1 || len(outages["nas-usv"]) != 2 {
		t.Fatalf("outages %v", outages)
	}
}

// TestOpenDTUOffline: an unreachable inverter counts in daylight only.
func TestOpenDTUOffline(t *testing.T) {
	data := sources.DemoSolar(time.Now())
	noon := todayEnv(nil)
	noon.Today = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if got := run(t, "opendtu.offline", data, noon); len(got) != 1 || got[0].Params["name"] != "Garage" {
		t.Fatalf("noon: %+v", got)
	}
	night := todayEnv(nil)
	night.Today = time.Date(2026, 7, 1, 23, 0, 0, 0, time.UTC)
	if got := run(t, "opendtu.offline", data, night); len(got) != 0 {
		t.Fatalf("night: %+v", got)
	}
}

// TestChargeExpensive: the car charges from the grid while Tibber's price
// is well above the day's average.
func TestChargeExpensive(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	tib := &sources.TibberDataset{Current: 0.40, Prices: []sources.PricePoint{{At: now.Add(-2 * time.Hour), Total: 0.20}, {At: now, Total: 0.40}, {At: now.Add(time.Hour), Total: 0.22}}}
	evcc := &sources.EVCCDataset{PV: 100, Grid: 3600, Loadpoints: []sources.Loadpoint{{Title: "Carport", Charging: true, ChargePower: 3700}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"tibber": tib, "evcc": evcc}
	if got := run(t, "cross.charge_expensive", nil, env); len(got) != 1 || got[0].Params["point"] != "Carport" {
		t.Fatalf("expensive: %+v", got)
	}
	tib.Current = 0.21
	if got := run(t, "cross.charge_expensive", nil, env); len(got) != 0 {
		t.Fatalf("cheap: %+v", got)
	}
}
