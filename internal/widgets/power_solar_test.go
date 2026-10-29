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
	e := sources.DemoHassEnergy(time.Now())
	results := map[string]any{powerHass: e, string(enums.ServiceOpenDTU): &sources.SolarDataset{Power: 1, YieldDay: 99000}}
	v := powerView(PowerConfig{}, results, ViewCtx{})
	if v["Any"] != true || v["YieldKWh"] == 99.0 || v["SelfRate"] == nil {
		t.Fatalf("view: %+v", v)
	}

	// Without a solar statistic the dataset counts as none.
	if v := powerView(PowerConfig{}, map[string]any{powerHass: &sources.HassEnergy{}}, ViewCtx{}); v["Any"] != false {
		t.Fatalf("empty: %+v", v)
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
