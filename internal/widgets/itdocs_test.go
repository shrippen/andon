package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// docsTasks are a docs dialog's task lists by label key.
func docsTasks(t *testing.T, d widgets.DetailView) map[string]widgets.Tasks {
	t.Helper()
	body, ok := d.Body.(*widgets.DetailBody)
	if !ok {
		t.Fatalf("body: %+v", d.Body)
	}
	out := map[string]widgets.Tasks{}
	for _, b := range body.Blocks {
		if tasks, ok := b.Data.(widgets.Tasks); ok {
			out[tasks.Label.Key] = tasks
		}
	}
	return out
}

// With the demo's Komodo the tile counts what it does not run and what it
// runs outside the repos, and the dialog lists both; without Komodo
// neither shows.
func TestDocsCoverageKomodo(t *testing.T) {
	now := time.Now()
	results := map[string]any{"data": sources.DemoGitea(now), "komodo": sources.DemoKomodo(now)}
	ctx := ctxFor(enums.ServiceGitea, nil)

	v := viewOf(t, "docs_coverage", nil, results, enums.ServiceGitea, nil)
	if v["NotDeployed"] != 1 || v["Unknown"] != 1 {
		t.Fatalf("view: %+v", v)
	}

	kind, _ := widgets.Get("docs_coverage")
	cfg, _ := widgets.Decode("docs_coverage", nil)
	deployed := docsTasks(t, kind.Detail(cfg, results, ctx))["detail.itdocs.deployed"]
	if deployed.Total != 4 || deployed.Done != 3 || len(deployed.Items) != 2 || deployed.Items[0].State != "bad" {
		t.Fatalf("tasks: %+v", deployed)
	}

	delete(results, "komodo")
	if v := viewOf(t, "docs_coverage", nil, results, enums.ServiceGitea, nil); v["NotDeployed"] != nil {
		t.Fatalf("without komodo: %+v", v)
	}
	if _, ok := docsTasks(t, kind.Detail(cfg, results, ctx))["detail.itdocs.deployed"]; ok {
		t.Fatal("without komodo: deploy tasks")
	}
}
