package widgets

import (
	"math"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// Home Assistant's statistics win over OpenDTU on the tile: they know
// all panels.
func TestPowerPrefersHass(t *testing.T) {
	// This month on the view's clock (server zone, no Zone): the demo's
	// month is empty before sunrise on the 1st.
	e := &sources.HassEnergy{Solar: []string{"pv"}, Export: []string{"out"},
		Months: []sources.SolarDay{{Day: time.Now().Format(solarMonthKey), Solar: 10, Export: 2}}}
	results := map[string]any{powerHass: e, string(enums.ServiceOpenDTU): &sources.SolarDataset{Power: 1, YieldDay: 99000}}
	v := powerView(PowerConfig{}, results, ViewCtx{})
	// Before the first yield of the day the PV line stays: the month has
	// its rate.
	if v["Any"] != true || v["YieldKWh"] != 0.0 || v["SelfRate"] == nil || v["Solar"] != true {
		t.Fatalf("view: %+v", v)
	}

	// Without a solar statistic the dataset counts as none.
	if v := powerView(PowerConfig{}, map[string]any{powerHass: &sources.HassEnergy{}}, ViewCtx{}); v["Any"] != false {
		t.Fatalf("empty: %+v", v)
	}
}

// On the spring clock change the hours follow the wall clock: 10:00
// CEST is hour 10, its label and the mark.
func TestSolarTodayClockChange(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	ten := time.Date(2026, 3, 29, 10, 0, 0, 0, loc)
	e := &sources.HassEnergy{Zone: "Europe/Berlin", Solar: []string{"pv"},
		Today:    []sources.SolarPoint{{At: ten.Add(-time.Hour), KWh: 0.1}, {At: ten, KWh: 0.2}},
		Forecast: []sources.SolarPoint{{At: ten.Add(-time.Hour), KWh: 0.5}, {At: ten, KWh: 1}}}
	b, ok := solarToday(e, ten.Add(10*time.Minute))
	g := b.Data.(Graph)
	if !ok || g.Mark != 10 || g.Labels[10] != "10:00" || g.Series[1].Values[10] != 1 || math.IsNaN(g.Series[0].Values[10]) {
		t.Fatalf("mark %d label %v values %v %v", g.Mark, g.Labels[10], g.Series[0].Values, g.Series[1].Values)
	}
}

// 30 different days in Home Assistant's zone, also across a clock
// change while the server's clock reads UTC.
func TestSolarDayKeys(t *testing.T) {
	e := &sources.HassEnergy{Zone: "Europe/Berlin"}
	keys := dayKeys(e, time.Date(2026, 11, 10, 22, 30, 0, 0, time.UTC))
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	if len(seen) != solarDays || keys[len(keys)-1] != "2026-11-10" {
		t.Fatalf("keys %v", keys)
	}
}

// The dialog's PV charts carry a legend, labels per value and a unit;
// the hours not yet come are gaps.
func TestSolarBlocks(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC) // 12:00 in Berlin
	e := sources.DemoHassEnergy(now)
	if facts := solarFacts(e, now); len(facts) != 5 {
		t.Fatalf("facts: %+v", facts)
	}
	blocks := solarBlocks(e, now)
	if len(blocks) != 4 || blocks[3].Kind != BlockTable {
		t.Fatalf("blocks: %d", len(blocks))
	}
	for _, b := range blocks[:3] {
		g := b.Data.(Graph)
		if g.Unit != "kWh" || len(g.Series) < 2 || len(g.Labels) != len(g.Series[0].Values) {
			t.Fatalf("%v: %+v", b.Label, g)
		}
		for _, s := range g.Series {
			if s.Label == nil {
				t.Fatalf("%v: series without legend", b.Label)
			}
		}
	}
	hours := blocks[0].Data.(Graph)
	if hours.Mark != 12 || math.IsNaN(hours.Series[0].Values[12]) || !math.IsNaN(hours.Series[0].Values[13]) {
		t.Fatalf("hours: mark %d %v", hours.Mark, hours.Series[0].Values)
	}
	if rows := blocks[3].Data.(Table).Rows; len(rows) != monthsPerYear {
		t.Fatalf("table rows %d", len(rows))
	}
}
