package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestDroneFailing: the demo's showreel is red since yesterday; the
// website is green again.
func TestDroneFailing(t *testing.T) {
	got := run(t, "drone.failing", sources.DemoDrone(time.Now()), todayEnv(nil))
	if len(got) != 1 || got[0].Params["repo"] != "studio/showreel" || got[0].Params["count"] != 2 {
		t.Fatalf("failing: %+v", got)
	}
}

// TestReleaseRedCI: the demo's website v0.12.0 went out after a red
// build of its branch; without CI history nothing is said.
func TestReleaseRedCI(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"github": sources.DemoGitHub(now), "drone": sources.DemoDrone(now)}
	got := run(t, "cross.release_red_ci", nil, env)
	if len(got) != 1 || got[0].Params["repo"] != "studio/website" || got[0].Params["release"] != "v0.12.0" || got[0].Params["build"] != 84 {
		t.Fatalf("release: %+v", got)
	}
	env.Datasets = map[string]any{"github": sources.DemoGitHub(now)}
	if got := run(t, "cross.release_red_ci", nil, env); len(got) != 0 {
		t.Fatalf("no history: %+v", got)
	}
}
