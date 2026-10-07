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

// TestCIRedDeployed: a deploy while the matching repo's CI was red. The
// repo comes from the option ci_repos, else the stack's git repo, else a
// CI repo named like the stack; a run of the deployed commit wins over
// the last run before the deploy.
func TestCIRedDeployed(t *testing.T) {
	at := time.Now().UTC().Add(-4 * time.Hour)
	drone := &sources.DroneDataset{Repos: []sources.DroneRepo{
		{Repo: "studio/showreel", Branch: "main", Builds: []sources.CIRun{
			{Number: 41, Status: sources.CIFailed, Started: at.Add(-time.Hour), Commit: "bad1234"},
			{Number: 40, Status: sources.CIOK, Started: at.Add(-3 * time.Hour), Commit: "good123"},
		}},
		{Repo: "studio/website", Builds: []sources.CIRun{{Number: 9, Status: sources.CIFailed, Started: at.Add(-time.Hour)}}},
	}}
	komodo := &sources.KomodoDataset{
		Stacks: []sources.KStack{{Name: "showreel"}, {Name: "web"}, {Name: "reel-old", Repo: "studio/showreel"}, {Name: "solo"}},
		Deployed: []sources.KDeployed{
			{Stack: "showreel", At: at, Commit: "bad1234", By: "mara", OK: true},
			{Stack: "web", At: at, By: "lena", OK: true},
			{Stack: "reel-old", At: at, Commit: "good123", OK: true}, // the green commit, though red was newer
			{Stack: "solo", At: at, OK: true},                        // no CI
			{Stack: "showreel", At: at.AddDate(0, 0, -30), OK: true},
		},
	}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"komodo": komodo, "drone": drone}
	env.Options = map[string]map[string]any{"komodo": {"ci_repos": map[string]any{"web": "studio/website"}}}

	got := run(t, "cross.ci_red_deployed", nil, env)
	if len(got) != 2 {
		t.Fatalf("found %+v", got)
	}
	if p := got[0].Params; p["stack"] != "showreel" || p["repo"] != "studio/showreel" || p["build"] != 41 || p["by"] != "mara" {
		t.Fatalf("showreel %+v", p)
	}
	if p := got[1].Params; p["stack"] != "web" || p["repo"] != "studio/website" || p["build"] != 9 {
		t.Fatalf("web %+v", p)
	}
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Fatal("same fingerprint")
	}
}

// TestCIRedDeployedDemo: the demo deployed the showreel an hour after
// its red build #41.
func TestCIRedDeployedDemo(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"komodo": sources.DemoKomodo(now), "drone": sources.DemoDrone(now)}
	got := run(t, "cross.ci_red_deployed", nil, env)
	if len(got) != 1 || got[0].Params["stack"] != "showreel" || got[0].Params["build"] != 41 {
		t.Fatalf("demo %+v", got)
	}
}
