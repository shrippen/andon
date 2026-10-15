package metrics_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// The update cases from the real Gitea, WUD, FreshRSS and NVD: matching
// against real compose files and release feeds. Only reads; the cases are logged for a look.
func TestUpdateCasesLive(t *testing.T) {
	in := metrics.UpdateInputs{}
	for _, inst := range live.Instances(t) {
		svc := enums.ServiceType(inst.Service)
		if svc != enums.ServiceWUD && svc != enums.ServiceGitea && svc != enums.ServiceNVD && svc != enums.ServiceFreshRSS {
			continue
		}
		if inst.Err != nil {
			t.Fatalf("%s login: %v", svc, inst.Err)
		}
		src, err := sources.Get(sources.DataKey(svc))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		out, err := src.Fetch(ctx, inst.Ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", inst.Name, err)
		}
		switch d := out.(type) {
		case *sources.WUDDataset:
			in.WUD = d
		case *sources.GiteaDataset:
			in.Gitea = d
		case *sources.NVDDataset:
			in.NVD = d
		case *sources.FreshRSSDataset:
			in.Feeds = d
		}
	}
	if in.Gitea == nil {
		t.Skip("no live Gitea instance")
	}
	t.Logf("stacks: %d, WUD: %v, feeds: %v", len(in.Gitea.Stacks), in.WUD != nil, in.Feeds != nil)
	if in.Feeds != nil {
		t.Logf("releases in feeds: %d", len(in.Feeds.Releases))
	}
	for _, c := range metrics.UpdateCases(in) {
		where := c.Container
		if c.Stack != nil {
			where = c.Stack.Repo + " " + c.Stack.Path
		}
		t.Logf("%-40s %s → %s urgency %d pinned %v %s %v", c.Image, c.Tag, c.NewTag, c.Urgency, c.Pinned, where, c.CVEs)
	}
	feeds := metrics.FeedCases(in.Gitea, in.Feeds)
	n := map[metrics.FeedAction]int{}
	for _, c := range feeds {
		n[c.Action]++
		if n[c.Action] <= 5 {
			t.Logf("feed %-7s %s %s %s", c.Action, c.Repo, c.Title, c.URL)
		}
	}
	t.Logf("feed proposals: %v", n)
}
