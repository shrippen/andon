package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestProjectMentioned: the demo's Show HN post links the studio's store
// entry; a repo link counts only for the owner's repos and only up to a
// separator; a store name counts in a title.
func TestProjectMentioned(t *testing.T) {
	now := time.Now().UTC()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"news": sources.DemoNews(now), "github": sources.DemoGitHub(now), "kdestore": sources.DemoKDEStore(now)}
	got := run(t, "cross.project_mentioned", nil, env)
	if len(got) != 1 || got[0].Params["project"] != "Timecode Clock" || got[0].ActionURL == "" {
		t.Fatalf("demo: %+v", got)
	}

	news := &sources.NewsDataset{Items: []sources.NewsItem{
		{Site: "hackernews", Title: "A", URL: "https://github.com/studio/website/pull/3", Link: "l1"},
		{Site: "reddit", Feed: "selfhosted", Title: "B", URL: "https://github.com/studio/websites", Link: "l2"},
		{Site: "lobsters", Title: "C", URL: "https://github.com/traefik/traefik", Link: "l3"},
		{Site: "youtube", Feed: "Club", Title: "Why I use shot list daily", URL: "https://youtu.be/x", Link: "l4"},
	}}
	gh := &sources.GitHubDataset{Owner: "Studio", Repos: []sources.GitRepo{{Name: "studio/website"}, {Name: "traefik/traefik"}}}
	store := &sources.KDEStoreDataset{Items: []sources.StoreItem{{ID: 1, Name: "Shot List"}, {ID: 2, Name: "Clock"}}}
	env.Datasets = map[string]any{"news": news, "github": gh, "kdestore": store}
	got = run(t, "cross.project_mentioned", nil, env)
	if len(got) != 2 || got[0].Params["project"] != "studio/website" || got[1].Params["project"] != "Shot List" || got[1].Params["site"] != "Club" {
		t.Fatalf("mentions: %+v", got)
	}
}
