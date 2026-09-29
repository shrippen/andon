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
