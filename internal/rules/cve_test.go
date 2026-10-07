package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestImageCVE: in the demo Gitea and Immich run affected versions,
// Paperless without a version tag needs a check; Immich is published by
// Pangolin, which raises its level. Without the NVD nothing is checked.
func TestImageCVE(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"nvd": sources.DemoNVD(now), "docker": sources.DemoDocker(), "gitea": sources.DemoGitea(now),
		"pangolin": sources.DemoPangolin()}
	got := run(t, "cross.image_cve", nil, env)
	byCVE := map[string]enums.Severity{}
	for _, f := range got {
		if s, seen := byCVE[f.Params["id"].(string)]; !seen || f.Severity > s {
			byCVE[f.Params["id"].(string)] = f.Severity
		}
	}
	if byCVE["CVE-2026-39954"] != enums.SeverityCritical || byCVE["CVE-2026-41207"] != enums.SeverityWarn || byCVE["CVE-2026-33018"] != enums.SeverityInfo {
		t.Fatalf("levels %v (%+v)", byCVE, got)
	}
	if _, ok := byCVE["CVE-2026-40112"]; ok {
		t.Fatal("vaultwarden runs nowhere in the demo")
	}

	delete(env.Datasets, "nvd")
	if got := run(t, "cross.image_cve", nil, env); len(got) != 0 {
		t.Fatalf("without NVD: %+v", got)
	}
}
