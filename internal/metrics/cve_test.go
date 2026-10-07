package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestImageCVEs: an image matches a CVE by product (immich-server ↔
// immich, kimai2 ↔ kimai); a tag below the fixed version is affected,
// one at or above it is not, one without a version needs a check.
func TestImageCVEs(t *testing.T) {
	images := []metrics.RunningImage{
		{Image: "ghcr.io/immich-app/immich-server:v1.131.0", Where: "immich-server"},
		{Image: "ghcr.io/immich-app/immich-server:v2.1", Where: "photos"},
		{Image: "gitea/gitea:1.24", Where: "gitea"},
		{Image: "paperlessngx/paperless-ngx", Where: "paperless"},
		{Image: "kimai/kimai2:apache-2.40", Where: "kimai"},
		{Image: "redis:8", Where: "redis"},
	}
	cves := []sources.CVE{
		{ID: "A", Score: 7.5, Products: []sources.CVEProduct{{Vendor: "immich", Product: "immich", To: "1.132.0"}}},
		{ID: "B", Score: 8.1, Products: []sources.CVEProduct{{Vendor: "gitea", Product: "gitea", From: "1.20.0", To: "1.24.3"}}},
		{ID: "C", Score: 5.4, Products: []sources.CVEProduct{{Vendor: "paperless-ngx", Product: "paperless-ngx", To: "2.15.0"}}},
		{ID: "D", Score: 9.0, Products: []sources.CVEProduct{{Vendor: "kimai", Product: "kimai", To: "2.40", ToIncluded: true}}},
		{ID: "E", Score: 9.8, Products: []sources.CVEProduct{{Vendor: "git", Product: "git", To: "2.50"}}},
	}
	got := map[string]metrics.CVEState{}
	for _, m := range metrics.ImageCVEs(images, cves) {
		got[m.CVE.ID+"@"+m.Image.Where] = m.State
	}
	want := map[string]metrics.CVEState{
		"A@immich-server": metrics.CVEAffected, "B@gitea": metrics.CVEAffected,
		"C@paperless": metrics.CVECheck, "D@kimai": metrics.CVEAffected,
	}
	if len(got) != len(want) {
		t.Fatalf("matches %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %s, want %s (all %v)", k, got[k], v, got)
		}
	}
}

// TestRunningImages: Docker containers that run and the compose stacks'
// services, with their host.
func TestRunningImages(t *testing.T) {
	now := time.Now()
	images := metrics.RunningImages(map[string]any{"docker": sources.DemoDocker(), "gitea": sources.DemoGitea(now)})
	hosts := map[string]bool{}
	for _, i := range images {
		if i.Image == "" {
			t.Fatalf("empty image %+v", i)
		}
		hosts[i.Host] = true
	}
	if len(images) < 5 || !hosts["boje"] {
		t.Fatalf("images %+v", images)
	}
}
