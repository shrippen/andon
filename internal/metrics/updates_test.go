package metrics

import (
	"testing"

	"andon/internal/sources"
)

// A newer tag is matched to the stack pinning the image; a CVE on the
// running tag ranks first, a published service next.
func TestUpdateCases(t *testing.T) {
	wud := &sources.WUDDataset{Containers: []sources.ImageUpdate{
		{Name: "immich_server", Image: "ghcr.io/immich-app/immich-server:v1.131.0", Tag: "v1.131.0", NewTag: "v1.132.3"},
		{Name: "gitea", Image: "gitea/gitea:1.24.0", Tag: "1.24.0", NewTag: "1.24.6"},
		{Name: "grafana", Image: "grafana/grafana:11.0.0", Tag: "11.0.0", NewTag: "11.1.0"},
		{Name: "redis", Image: "redis:7", Tag: "7"},
	}}
	gitea := &sources.GiteaDataset{Stacks: []sources.Stack{
		{Host: "regis", Name: "immich", Repo: "alex/docker-compose-regis", Path: "immich/compose.yaml",
			Services: []sources.ComposeService{{Name: "server", Image: "ghcr.io/immich-app/immich-server:v1.131.0"}}},
		{Host: "regis", Name: "gitea", Repo: "alex/docker-compose-regis", Path: "gitea/compose.yaml",
			Services: []sources.ComposeService{{Name: "gitea", Image: "docker.io/gitea/gitea:${GITEA_VERSION}"}}},
	}}
	nvd := &sources.NVDDataset{CVEs: []sources.CVE{{ID: "CVE-2026-1", Score: 8.1, Products: []sources.CVEProduct{{Product: "gitea", To: "1.24.5", ToIncluded: true}}}}}

	got := UpdateCases(UpdateInputs{WUD: wud, Gitea: gitea, NVD: nvd, Public: []string{"photos.example.org", "immich"}})
	if len(got) != 3 {
		t.Fatalf("cases: %+v", got)
	}
	if c := got[0]; c.Urgency != UrgencyCVE || c.Image != "gitea/gitea" || c.Pinned || c.Stack == nil || c.CVEs[0] != "CVE-2026-1" {
		t.Errorf("gitea: %+v", c)
	}
	if c := got[1]; c.Urgency != UrgencyPublic || !c.Pinned || c.Service != "server" || c.Key != "u-regis-immich-server" {
		t.Errorf("immich: %+v", c)
	}
	if c := got[2]; c.Urgency != UrgencyNormal || c.Stack != nil || c.Key != "u-grafana" {
		t.Errorf("grafana: %+v", c)
	}
}

// A release feed proposes a newer version for a pinned image, in the
// running tag's form; an older or same release proposes nothing.
func TestFeedCases(t *testing.T) {
	gitea := &sources.GiteaDataset{Stacks: []sources.Stack{{Host: "h", Name: "kimai", Services: []sources.ComposeService{
		{Name: "kimai", Image: "kimai/kimai2:apache-2.40"}, {Name: "db", Image: "mariadb:11.4"}}},
		{Host: "h", Name: "immich", Services: []sources.ComposeService{{Name: "server", Image: "ghcr.io/immich-app/immich-server:v2.1"}}}}}
	feeds := &sources.FreshRSSDataset{Releases: []sources.FeedRelease{
		{Repo: "kimai/kimai", Version: "2.41.0"}, {Repo: "immich-app/immich", Version: "v2.1.0"}}}
	got := UpdateCases(UpdateInputs{Gitea: gitea, Feeds: feeds})
	if len(got) != 1 || got[0].NewTag != "apache-2.41" || got[0].Service != "kimai" || !got[0].Pinned {
		t.Fatalf("cases: %+v", got)
	}
	for tag, want := range map[string]string{"v2.1": "v2.2", "1.4": "1.5", "31-apache": "32-apache", "11.4": ""} {
		release := map[string]string{"v2.1": "v2.2.0", "1.4": "v1.5.0", "31-apache": "v32.0.1", "11.4": "11.4.2"}[tag]
		if got, _ := newerTag(tag, release); got != want {
			t.Errorf("newerTag(%q, %q) = %q, want %q", tag, release, got, want)
		}
	}
}

// What's Up Docker names Docker Hub's official images "library/nginx",
// compose files mostly "nginx": the case keeps the form the file uses,
// so the pull request finds the line to change.
func TestUpdateCasesKeepComposeForm(t *testing.T) {
	wud := &sources.WUDDataset{Containers: []sources.ImageUpdate{{Name: "web", Image: "library/nginx:1.25.0", Tag: "1.25.0", NewTag: "1.31.7"}}}
	gitea := &sources.GiteaDataset{Stacks: []sources.Stack{{Host: "testhost", Name: "web", Repo: "a/docker-compose-testhost", Path: "web/compose.yaml",
		Services: []sources.ComposeService{{Name: "web", Image: "nginx:1.25.0"}}}}}
	cases := UpdateCases(UpdateInputs{WUD: wud, Gitea: gitea})
	if len(cases) != 1 || !cases[0].Pinned || cases[0].Written != "nginx" {
		t.Fatalf("cases: %+v", cases)
	}
}
