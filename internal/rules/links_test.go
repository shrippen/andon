package rules_test

import (
	"testing"

	"andon/internal/rules"
	"andon/internal/sources"
)

func TestLinksAgainstLinkwardenAndKuma(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		rules.LinksDataset: []rules.Link{
			{Title: "Kimai", URL: "https://kimai.org"},
			{Title: "Invoice Ninja", URL: "https://invoiceninja.com/"},
			{Title: "Wiki", URL: "https://wiki.lan"},
		},
		"linkwarden": sources.DemoLinkwarden(),
		"uptimekuma": &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "Kimai", Target: "https://www.kimai.org/login"}}},
	}

	if got := run(t, "linkwarden.not_on_board", nil, env); len(got) != 1 || got[0].Params["names"] != "Grafana" {
		t.Fatalf("not on board: %+v", got)
	}
	if got := run(t, "linkwarden.not_saved", nil, env); len(got) != 1 || got[0].Params["names"] != "Wiki" {
		t.Fatalf("not saved: %+v", got)
	}
	if got := run(t, "kuma.unmonitored", nil, env); len(got) != 1 || got[0].Params["names"] != "Invoice Ninja, Wiki" {
		t.Fatalf("unmonitored: %+v", got)
	}
	ignored := todayEnv(map[string]any{"rules": map[string]any{"kuma.unmonitored": map[string]any{"ignore": []any{"invoiceninja.com", "wiki.lan"}}}})
	ignored.Datasets = env.Datasets
	if got := run(t, "kuma.unmonitored", nil, ignored); len(got) != 0 {
		t.Fatalf("ignored hosts: %+v", got)
	}
}

// A bookmark without a name shows by its address, not as an empty
// entry (", , , ").
func TestNotOnBoardNamesUntitled(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		rules.LinksDataset: []rules.Link{},
		"linkwarden": &sources.LinkwardenDataset{Links: []sources.Bookmark{
			{URL: "https://www.example.org/docs/", Collection: "Arbeit"},
			{Name: "Kayak", URL: "https://kayak.de", Collection: "Arbeit"},
			{Name: "Kayak", URL: "https://www.kayak.de/", Collection: "Arbeit"}}},
	}
	// The same address saved twice counts once.
	if got := run(t, "linkwarden.not_on_board", nil, env); len(got) != 1 || got[0].Params["names"] != "Kayak, example.org/docs" || got[0].Params["count"] != 2 {
		t.Fatalf("not on board: %+v", got)
	}
}

func TestDeadLinks(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{rules.LinksDataset: []rules.Link{{Title: "Alt", URL: "https://a", DownDays: 9}, {Title: "Neu", URL: "https://b", DownDays: 2}}}
	got := run(t, "links.dead", nil, env)
	if len(got) != 1 || got[0].Params["names"] != "Alt" {
		t.Fatalf("dead: %+v", got)
	}
}
