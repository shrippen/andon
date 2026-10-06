package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

const regisRepo = "https://git.example/alex/docker-compose-regis/src/branch/main/"

// docsData: immich is linked by its note, the gotify note is deprecated
// but its stack still exists, kometa and newt have no note (newt is a
// device's infrastructure stack: linked from the device note, so it
// counts), and the Paperless note links a stack that is gone.
func docsData() *sources.GiteaDataset {
	stack := func(name string) sources.Stack {
		return sources.Stack{Host: "regis", Name: name, Repo: "alex/docker-compose-regis", Path: name + "/compose.yaml", URL: regisRepo + name + "/compose.yaml"}
	}
	return &sources.GiteaDataset{
		URL:        "https://git.example",
		StacksRead: true, NotesRead: true,
		Stacks: []sources.Stack{stack("gotify"), stack("immich"), stack("kometa"), stack("newt-regis"), stack("seerr")},
		Notes: []sources.DocNote{
			{Path: "IT/Dienste/Regis/Immich.md", Name: "Immich", Compose: []string{regisRepo + "immich/compose.yaml"}},
			{Path: "IT/Dienste/Regis/deprecated/Gotify.md", Name: "Gotify", Deprecated: true, Compose: []string{regisRepo + "gotify/compose.yaml"}},
			{Path: "IT/Dienste/Regis/Paperless.md", Name: "Paperless", Compose: []string{regisRepo + "paperless/compose.yaml"}},
			{Path: "IT/Dienste/Regis/Jellyseer.md", Name: "Jellyseer", Compose: []string{"https://git.example/alex/docker-compose-regis/raw/branch/main/seerr"}},
			{Path: "IT/Geräte/Regis.md", Name: "Regis", Compose: []string{regisRepo + "newt-regis/compose.yaml", "https://github.com/x/y"}},
		},
	}
}

func TestDocsMissing(t *testing.T) {
	got := run(t, "docs.missing", docsData(), todayEnv(nil))
	if len(got) != 1 || got[0].Params["host"] != "regis" || got[0].Params["count"] != 2 || got[0].Params["names"] != "gotify, kometa" {
		t.Fatalf("missing: %+v", got)
	}
}

func TestDocsOrphan(t *testing.T) {
	got := run(t, "docs.orphan", docsData(), todayEnv(nil))
	if len(got) != 1 || got[0].Params["note"] != "Paperless" || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("orphan: %+v", got)
	}
}

func TestDocsDeprecatedLive(t *testing.T) {
	got := run(t, "docs.deprecated_live", docsData(), todayEnv(nil))
	if len(got) != 1 || got[0].Params["note"] != "Gotify" || got[0].Params["stack"] != "gotify" {
		t.Fatalf("deprecated live: %+v", got)
	}
}

// Stacks or notes not read completely: no finding, an unread vault is
// no gap in it.
func TestDocsUnread(t *testing.T) {
	data := docsData()
	data.NotesRead = false
	for _, id := range []string{"docs.missing", "docs.orphan", "docs.deprecated_live"} {
		if got := run(t, id, data, todayEnv(nil)); len(got) != 0 {
			t.Fatalf("%s: %+v", id, got)
		}
	}
}

// The demo tells the story of the world's code note: gitea, kimai and
// stirling-pdf undocumented, dawarich only in a deprecated note,
// paperless-ai linked but gone.
func TestDocsDemo(t *testing.T) {
	data := sources.DemoGitea(time.Now())
	env := todayEnv(nil)

	names := map[any]any{}
	for _, f := range run(t, "docs.missing", data, env) {
		names[f.Params["host"]] = f.Params["names"]
	}
	if names["nebelhorn"] != "gitea" || names["feuerschiff"] != "kimai, stirling-pdf" || names["boje"] != "dawarich" || len(names) != 3 {
		t.Fatalf("missing: %v", names)
	}
	if got := run(t, "docs.orphan", data, env); len(got) != 1 || got[0].Params["note"] != "Paperless-ngx" {
		t.Fatalf("orphan: %+v", got)
	}
	if got := run(t, "docs.deprecated_live", data, env); len(got) != 1 || got[0].Params["stack"] != "dawarich" {
		t.Fatalf("deprecated live: %+v", got)
	}
}

// Findings Hansei claims wait: kometa's hint names only the rest and
// counts the claimed one, a claimed orphan or deprecated note is quiet.
func TestDocsClaimedByHansei(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{string(enums.ServiceHansei): &sources.HanseiDataset{Claimed: []string{
		"docs.missing:regis/kometa", "docs.orphan:IT/Dienste/Regis/Paperless.md", "docs.deprecated_live:regis/gotify"}}}

	missing := run(t, "docs.missing", docsData(), env)
	if len(missing) != 1 || missing[0].Params["names"] != "gotify" || missing[0].Params["count"] != 1 || missing[0].Params["claimed"] != 1 || missing[0].Message != "docs.missing_claimed" {
		t.Fatalf("missing: %+v", missing)
	}
	for _, id := range []string{"docs.orphan", "docs.deprecated_live"} {
		if got := run(t, id, docsData(), env); len(got) != 0 {
			t.Fatalf("%s: %+v", id, got)
		}
	}

	// The host's first gap claimed: the host still has its hint.
	env.Datasets[string(enums.ServiceHansei)] = &sources.HanseiDataset{Claimed: []string{"docs.missing:regis/gotify"}}
	if got := run(t, "docs.missing", docsData(), env); len(got) != 1 || got[0].Params["names"] != "kometa" {
		t.Fatalf("first claimed: %+v", got)
	}

	// All of a host's gaps claimed: no hint for it.
	env.Datasets[string(enums.ServiceHansei)] = &sources.HanseiDataset{Claimed: []string{"docs.missing:regis/kometa", "docs.missing:regis/gotify"}}
	if got := run(t, "docs.missing", docsData(), env); len(got) != 0 {
		t.Fatalf("all claimed: %+v", got)
	}
}
