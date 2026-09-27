package rules_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// A filling disk warns at 85 %, is critical at 95 %; snap images are
// always full and never count.
func TestGlancesDiskFull(t *testing.T) {
	data := &sources.GlancesResult{Disks: []sources.GlancesDisk{
		{Mount: "/", Percent: 52}, {Mount: "/data", Percent: 87}, {Mount: "/backup", Percent: 97},
		{Mount: "/snap/core/123", Percent: 100},
	}}
	found := run(t, "glances.disk_full", data, rules.Env{Today: day("2026-09-01")})
	if len(found) != 2 || found[0].Severity != enums.SeverityWarn || found[1].Severity != enums.SeverityCritical {
		t.Fatalf("findings: %+v", found)
	}
}

// Load is judged per core: 6 on 4 cores warns, 2 on 4 cores does not.
func TestGlancesLoadHigh(t *testing.T) {
	calm := run(t, "glances.load_high", &sources.GlancesResult{Load: 2, Cores: 4}, rules.Env{})
	busy := run(t, "glances.load_high", &sources.GlancesResult{Load: 6, Cores: 4}, rules.Env{})
	unknown := run(t, "glances.load_high", &sources.GlancesResult{Load: 6}, rules.Env{})
	if len(calm) != 0 || len(busy) != 1 || len(unknown) != 0 {
		t.Fatalf("calm %v busy %v unknown %v", calm, busy, unknown)
	}
}

// Heavy swapping slows everything; a little swap is normal.
func TestGlancesSwapHigh(t *testing.T) {
	low := run(t, "glances.swap_high", &sources.GlancesResult{Swap: 20}, rules.Env{})
	high := run(t, "glances.swap_high", &sources.GlancesResult{Swap: 85}, rules.Env{})
	if len(low) != 0 || len(high) != 1 {
		t.Fatalf("low %v high %v", low, high)
	}
}
