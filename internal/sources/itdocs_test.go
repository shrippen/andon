package sources_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/sources"
)

const immichNote = `---
tags:
  - Dienst
Gerät:
  - "[[Regis & Dettlaf]]"
deprecated: false
URL: https://im.example.org
Compose:
  - https://git.example/alex/docker-compose-regis/src/branch/main/immich/compose.yaml
externe Ports: 2283
interne Ports:
  - 2283
  - 5432
SSO möglich: true
SSO aktiv: false
Backup via:
  - "[[IT/Allgemeines/Borg|Borg]]"
abhängig von: "[[Authentik]]"
letzte Prüfung: 2026-09-01
---
# Immich

Passwort: hunter2
`

const oldNote = `---
Gerät: "[[IT/Geräte/Eredin]]"
deprecated: true
---
`

const deviceNote = `---
tags: [Gerät]
Ort:
  - Weimar
---
`

const hanseiNote = `---
hansei_review: 2
hansei_feedback: 1
hansei_done: 7
hansei_conformity: 0.82
hansei_updated: 2026-10-07T09:30:00+02:00
---
# Hansei-Status
`

// docsServer is a Gitea whose vault has IT/Dienste (with a deprecated
// note and a picture) and IT/Geräte; other folders are never listed.
func docsServer(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]any{
		"/api/v1/user":                map[string]any{"login": "alex"},
		"/api/v1/user/repos":          []any{},
		"/api/v1/repos/issues/search": []any{},
		"/api/v1/repos/alex/vault":    map[string]any{"default_branch": "main", "html_url": "https://git.example/alex/vault"},
		"/api/v1/repos/alex/vault/contents/IT": []any{
			map[string]any{"name": "Dienste", "type": "dir", "sha": "d1"},
			map[string]any{"name": "Geräte", "type": "dir", "sha": "d2"},
			map[string]any{"name": "Privat", "type": "dir", "sha": "d3"},
		},
		"/api/v1/repos/alex/vault/git/trees/d1": map[string]any{"truncated": false, "tree": []any{
			map[string]any{"path": "Regis", "type": "tree", "sha": "x"},
			map[string]any{"path": "Regis/Immich.md", "type": "blob", "sha": "n1"},
			map[string]any{"path": "Eredin/deprecated/Gotify.md", "type": "blob", "sha": "n2"},
			map[string]any{"path": "Regis/immich.png", "type": "blob", "sha": "p1"},
		}},
		"/api/v1/repos/alex/vault/contents/IT/Hansei-Status.md": map[string]any{"type": "file", "encoding": "base64",
			"html_url": "https://git.example/alex/vault/src/branch/main/IT/Hansei-Status.md",
			"content":  base64.StdEncoding.EncodeToString([]byte(hanseiNote))},
		"/api/v1/repos/alex/vault/git/trees/d2": map[string]any{"truncated": false, "tree": []any{
			map[string]any{"path": "Regis & Dettlaf.md", "type": "blob", "sha": "n3"},
		}},
	}
	files := map[string]string{"n1": immichNote, "n2": oldNote, "n3": deviceNote}

	mux := http.NewServeMux()
	for path, body := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(body)
		})
	}
	mux.HandleFunc("/api/v1/repos/alex/vault/git/blobs/{sha}", func(w http.ResponseWriter, r *http.Request) {
		text, ok := files[r.PathValue("sha")]
		if !ok {
			t.Errorf("read %s", r.PathValue("sha"))
		}
		json.NewEncoder(w).Encode(map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text))})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestGiteaReadsITDocs: the notes below docs_paths of docs_repo become
// notes with their frontmatter; links become note names, the body is
// never kept.
func TestGiteaReadsITDocs(t *testing.T) {
	srv := docsServer(t)
	sctx := sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true,
		Options: map[string]any{"docs_repo": "alex/vault", "docs_paths": []any{"IT/Dienste", "IT/Geräte"}}}

	out, err := sources.GiteaData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.GiteaDataset)
	if !d.NotesRead || len(d.Notes) != 3 {
		t.Fatalf("notes: %+v", d.Notes)
	}

	old, immich, device := d.Notes[0], d.Notes[1], d.Notes[2]
	if old.Path != "IT/Dienste/Eredin/deprecated/Gotify.md" || !old.Deprecated || len(old.Devices) != 1 || old.Devices[0] != "Eredin" {
		t.Fatalf("old: %+v", old)
	}
	if immich.Name != "Immich" || immich.Deprecated || immich.Devices[0] != "Regis & Dettlaf" || immich.Web != "https://im.example.org" {
		t.Fatalf("immich: %+v", immich)
	}
	if immich.URL != "https://git.example/alex/vault/src/branch/main/IT/Dienste/Regis/Immich.md" {
		t.Fatalf("url: %s", immich.URL)
	}
	if len(immich.Compose) != 1 || !strings.HasSuffix(immich.Compose[0], "/immich/compose.yaml") {
		t.Fatalf("compose: %v", immich.Compose)
	}
	if strings.Join(immich.ExternalPorts, ",") != "2283" || strings.Join(immich.InternalPorts, ",") != "2283,5432" {
		t.Fatalf("ports: %v %v", immich.ExternalPorts, immich.InternalPorts)
	}
	if !immich.SSOPossible || immich.SSOActive || immich.Backup[0] != "Borg" || immich.DependsOn[0] != "Authentik" || immich.Checked != "2026-09-01" {
		t.Fatalf("immich: %+v", immich)
	}
	if device.Name != "Regis & Dettlaf" || device.Tags[0] != "Gerät" || device.Places[0] != "Weimar" {
		t.Fatalf("device: %+v", device)
	}

	if raw, _ := json.Marshal(d); strings.Contains(string(raw), "hunter2") {
		t.Fatal("note body kept")
	}
}

// TestGiteaWithoutDocsRepo: without docs_repo no note is read.
func TestGiteaWithoutDocsRepo(t *testing.T) {
	srv := docsServer(t)

	out, err := sources.GiteaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if d := out.(*sources.GiteaDataset); d.NotesRead || len(d.Notes) != 0 {
		t.Fatalf("notes: %+v", d.Notes)
	}
}

// TestGiteaReadsHanseiStatus: hansei_note names Hansei's status note in
// the vault; its counts become the dataset's Hansei status.
func TestGiteaReadsHanseiStatus(t *testing.T) {
	srv := docsServer(t)
	sctx := sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true,
		Options: map[string]any{"docs_repo": "alex/vault", "docs_paths": []any{"IT/Geräte"}, "hansei_note": "IT/Hansei-Status.md"}}

	out, err := sources.GiteaData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	h := out.(*sources.GiteaDataset).Hansei
	if h == nil || h.Review != 2 || h.Feedback != 1 || h.Done != 7 || h.Conformity != 0.82 || h.Updated.IsZero() || !strings.HasSuffix(h.URL, "/IT/Hansei-Status.md") {
		t.Fatalf("hansei: %+v", h)
	}
}

// The demo has Hansei's status note: two batches to review, one with
// questions, one done, half conformant (world code.gitea.hansei).
func TestDemoGiteaHansei(t *testing.T) {
	h := sources.DemoGitea(time.Now()).Hansei
	if h == nil || h.Review != 2 || h.Feedback != 1 || h.Done != 1 || h.Conformity != 0.5 || h.Updated.IsZero() {
		t.Fatalf("hansei: %+v", h)
	}
}
