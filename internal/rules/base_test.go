package rules

import "testing"

// Negative amounts (credits, refunds) round away from zero like positive ones.
func TestRound2Negative(t *testing.T) {
	if got := round2(-1.236); got != -1.24 {
		t.Fatalf("round2(-1.236) = %v, want -1.24", got)
	}
}

// A finding without Rule gets the id of the rule that produced it.
func TestOwnRuleStampsID(t *testing.T) {
	run := ownRule("x.y", func(any, map[string]any, Env) []Finding {
		return []Finding{{}, {Rule: "other"}}
	})
	found := run(nil, nil, Env{})
	if found[0].Rule != "x.y" || found[1].Rule != "other" {
		t.Fatalf("rules = %q, %q", found[0].Rule, found[1].Rule)
	}
}

// on runs a rule only on a non-nil dataset of its type.
func TestOnSkipsOtherData(t *testing.T) {
	run := on(func(data *Clock, _ map[string]any, _ Env) []Finding {
		return []Finding{{Fingerprint: data.Host}}
	})

	var none *Clock
	for _, raw := range []any{"other", none, nil} {
		if found := run(raw, nil, Env{}); found != nil {
			t.Fatalf("run(%#v) = %v, want nil", raw, found)
		}
	}

	found := run(&Clock{Host: "nas"}, nil, Env{})
	if len(found) != 1 || found[0].Fingerprint != "nas" {
		t.Fatalf("found = %v", found)
	}
}

// TestSetting: a space's override wins over the default; unknown rules
// and keys are 0.
func TestSetting(t *testing.T) {
	if got := Setting(nil, "hass.battery_low", "warn"); got != 20 {
		t.Fatalf("default: %v", got)
	}
	own := map[string]any{"rules": map[string]any{"hass.battery_low": map[string]any{"warn": 30.0}}}
	if got := Setting(own, "hass.battery_low", "warn"); got != 30 {
		t.Fatalf("override: %v", got)
	}
	if Setting(own, "no.such", "warn") != 0 || Setting(own, "hass.battery_low", "nope") != 0 {
		t.Fatal("unknown not 0")
	}
}
