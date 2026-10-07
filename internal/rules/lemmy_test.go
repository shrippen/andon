package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestLemmyRules: the demo's unread reply; a hot post linking the clock
// is a mention; without Mastodon posts the website and the clock are
// unannounced, until an own Lemmy post names the website.
func TestLemmyRules(t *testing.T) {
	now := time.Now().UTC()
	lemmy := sources.DemoLemmy(now)
	env := todayEnv(nil)
	if got := run(t, "lemmy.replies", lemmy, env); len(got) != 1 || got[0].Params["count"] != 1 {
		t.Fatalf("replies: %+v", got)
	}

	env.Datasets = map[string]any{"lemmy": lemmy, "github": sources.DemoGitHub(now), "kdestore": sources.DemoKDEStore(now)}
	got := run(t, "cross.project_mentioned", nil, env)
	if len(got) != 1 || got[0].Params["project"] != "Timecode Clock" || got[0].Params["site"] != "c/kde" || got[0].Sources[0] != "lemmy" {
		t.Fatalf("mentioned: %+v", got)
	}

	if got := run(t, "cross.release_unannounced", nil, env); len(got) != 2 {
		t.Fatalf("unannounced: %+v", got)
	}
	lemmy.Own = append(lemmy.Own, sources.LemmyPost{Title: "Our website v0.12 is live", At: now.AddDate(0, 0, -5)})
	if got := run(t, "cross.release_unannounced", nil, env); len(got) != 1 || got[0].Params["project"] != "Timecode Clock" {
		t.Fatalf("announced on Lemmy: %+v", got)
	}
}
