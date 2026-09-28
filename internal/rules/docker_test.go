package rules_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
)

// An unhealthy container warns, one restarting in a loop or crashed
// (non-zero exit) is critical; a job that ended with 0 is fine.
func TestDockerRules(t *testing.T) {
	data := sources.DemoDocker()
	data.Containers = append(data.Containers, sources.Container{Name: "loop", State: sources.StateRestarting})
	env := todayEnv(nil)

	unhealthy := run(t, "docker.unhealthy", data, env)
	if len(unhealthy) != 1 || unhealthy[0].Params["name"] != "paperless" || unhealthy[0].Severity != enums.SeverityWarn {
		t.Fatalf("unhealthy: %+v", unhealthy)
	}
	down := run(t, "docker.crashed", data, env)
	names := map[any]bool{}
	for _, f := range down {
		names[f.Params["name"]] = true
	}
	if len(down) != 2 || !names["loop"] || !names["umami"] {
		t.Fatalf("crashed: %+v", down)
	}
}
