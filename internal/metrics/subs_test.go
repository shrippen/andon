package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// subActions are the cases' actions by key.
func subActions(cases []SubCase) map[string]SubAction {
	out := map[string]SubAction{}
	for _, c := range cases {
		out[c.Key] = c.Action
	}
	return out
}

func TestSubCasesDemo(t *testing.T) {
	now := time.Now()
	cases := SubCases(SubInputs{Bank: sources.DemoSure(now), Wallos: sources.DemoWallos(now), Ninja: sources.DemoNinja(now), Today: now})
	want := map[string]SubAction{"wp-1": SubWallosPrice, "wm-5": SubWallosMerge, "wa-farbraum-grade-abo": SubWallosAdd, "np-demo-r1": SubNinjaPrice}
	got := subActions(cases)
	for k, a := range want {
		if got[k] != a {
			t.Errorf("case %s = %q, want %q (all: %v)", k, got[k], a, got)
		}
	}
	for _, c := range cases {
		if c.Key == "np-demo-r1" && (c.Old != 41.65 || c.Amount != 44.9 || !c.Ticked) {
			t.Errorf("price case %+v", c)
		}
	}
}
