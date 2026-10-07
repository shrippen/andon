package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestSeerrStuck: the demo's two requests wait for days; with Sonarr's
// own problems at the same time, the cross check names both.
func TestSeerrStuck(t *testing.T) {
	now := time.Now()
	seerr := sources.DemoSeerr(now)
	if got := run(t, "seerr.stuck", seerr, todayEnv(nil)); len(got) != 1 || got[0].Params["count"] != 2 {
		t.Fatalf("stuck: %+v", got)
	}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"seerr": seerr, "arr": &sources.ArrDataset{App: "Sonarr", Health: []sources.ArrHealth{{Message: "Indexer unavailable"}}}}
	if got := run(t, "cross.requests_arr", nil, env); len(got) != 1 || got[0].Params["app"] != "Sonarr" {
		t.Fatalf("cross: %+v", got)
	}
	env.Datasets["arr"] = &sources.ArrDataset{App: "Sonarr"}
	if got := run(t, "cross.requests_arr", nil, env); len(got) != 0 {
		t.Fatalf("healthy arr: %+v", got)
	}
}
