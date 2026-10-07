package itdocs_test

import (
	"context"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/itdocs"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// The findings are per stack and note, from the stored Gitea dataset of
// the caller's connections; a stranger gets none and no complete read.
func TestFindingsPerStack(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	ctx := context.Background()

	id := testkit.Conn(t, d, who, space, enums.ServiceGitea, "demo://git")
	conn, err := content.Connection(d, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceGitea), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
		t.Fatal(err)
	}

	report, err := itdocs.Findings(ctx, d, who)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete {
		t.Fatal("not complete")
	}

	byKey := map[string]itdocs.Finding{}
	for _, f := range report.Findings {
		byKey[f.Rule+"|"+f.Host+"|"+f.Stack+"|"+f.Note] = f
	}
	gitea, ok := byKey["docs.missing|nebelhorn|gitea|"]
	if gitea.ID != "docs.missing:nebelhorn/gitea" {
		t.Fatalf("id: %q", gitea.ID)
	}
	if !ok || len(gitea.Services) != 1 || gitea.Services[0].Image == "" || len(gitea.Services[0].Ports) != 2 || gitea.Compose == "" {
		t.Fatalf("missing gitea: %+v in %+v", gitea, report.Findings)
	}
	if f, ok := byKey["docs.orphan|||Paperless-ngx"]; !ok || f.Link == "" || f.Path == "" {
		t.Fatalf("orphan: %+v", report.Findings)
	}
	if _, ok := byKey["docs.deprecated_live|boje|dawarich|Dawarich"]; !ok {
		t.Fatalf("deprecated live: %+v", report.Findings)
	}
	ninja, ok := byKey["docs.drift|||Invoice Ninja"]
	if !ok || len(ninja.Changes) != 2 || ninja.Changes[1].Old != "8002" || ninja.Changes[1].New != "8012" || ninja.ID != "docs.drift:IT/Dienste/Feuerschiff/Invoice Ninja.md" {
		t.Fatalf("drift: %+v", ninja)
	}
	if len(report.Findings) != 8 {
		t.Fatalf("findings: %d, want 4 missing + orphan + deprecated + 2 drifts", len(report.Findings))
	}

	// With Komodo in the space: what it does not run and what it runs
	// outside the repos, with the images it runs.
	kid := testkit.Conn(t, d, who, space, enums.ServiceKomodo, "demo://komodo")
	komodo, err := content.Connection(d, kid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKomodo), nil, komodo, model.UserHolder(who.UserID), svcdata.Force); err != nil {
		t.Fatal(err)
	}
	report, err = itdocs.Findings(ctx, d, who)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]itdocs.Finding{}
	for _, f := range report.Findings {
		rules[f.ID] = f
	}
	if f, ok := rules["docs.not_deployed:nebelhorn/newt-nebelhorn"]; !ok || f.Compose == "" || len(f.Services) != 1 {
		t.Fatalf("not deployed: %+v", report.Findings)
	}
	if f := rules["docs.drift:IT/Dienste/Nebelhorn/Immich.md"]; len(f.Changes) != 1 || f.Changes[0].From != "komodo" {
		t.Fatalf("running drift: %+v", report.Findings)
	}
	if f, ok := rules["docs.deployed_unknown:nebelhorn/paperless-ai"]; !ok || len(f.Services) != 1 || !report.Complete || len(report.Findings) != 11 {
		t.Fatalf("deployed unknown: %+v", report.Findings)
	}

	other, err := itdocs.Findings(ctx, d, stranger)
	if err != nil {
		t.Fatal(err)
	}
	if other.Complete || len(other.Findings) != 0 {
		t.Fatalf("stranger: %+v", other)
	}
}
