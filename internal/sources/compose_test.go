package sources_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"andon/internal/sources"
)

const immichCompose = `
x-common: &common
  restart: unless-stopped
services:
  server:
    <<: *common
    image: ghcr.io/immich-app/immich-server:v1.120
    ports: ["2283:2283", 9000]
    environment:
      DB_PASSWORD: hunter2
    labels:
      - homepage.name=Immich
  db:
    image: postgres:16
    ports:
      - target: 5432
        published: 5433
        protocol: tcp
    labels:
      backup: "true"
`

// composeServer is a Gitea with two compose repos and one other; blobs
// counts the compose files read.
func composeServer(t *testing.T, blobs *atomic.Int32) *httptest.Server {
	t.Helper()
	routes := map[string]any{
		"/api/v1/user":                map[string]any{"login": "alex"},
		"/api/v1/user/repos":          []any{},
		"/api/v1/repos/issues/search": []any{},
		"/api/v1/repos/search": map[string]any{"ok": true, "data": []any{
			map[string]any{"full_name": "alex/docker-compose-regis", "name": "docker-compose-regis", "default_branch": "main", "html_url": "https://git.example/alex/docker-compose-regis"},
			map[string]any{"full_name": "alex/docker-compose-ploetze", "name": "docker-compose-ploetze", "default_branch": "main", "html_url": "https://git.example/alex/docker-compose-ploetze"},
			map[string]any{"full_name": "alex/docker-compose-old", "name": "docker-compose-old", "archived": true},
			map[string]any{"full_name": "alex/notes-docker-compose-x", "name": "notes-docker-compose-x"},
		}},
		"/api/v1/repos/alex/docker-compose-regis/git/trees/main": map[string]any{"truncated": false, "tree": []any{
			map[string]any{"path": "README.md", "type": "blob", "sha": "r"},
			map[string]any{"path": "immich", "type": "tree", "sha": "t1"},
			map[string]any{"path": "immich/compose.yaml", "type": "blob", "sha": "b1"},
			map[string]any{"path": "immich/.env", "type": "blob", "sha": "e1"},
			map[string]any{"path": "immich/docker-compose.yml", "type": "blob", "sha": "b9"},
			map[string]any{"path": "broken/compose.yml", "type": "blob", "sha": "b2"},
			map[string]any{"path": "deep/sub/compose.yaml", "type": "blob", "sha": "b3"},
		}},
		"/api/v1/repos/alex/docker-compose-ploetze/git/trees/main": map[string]any{"truncated": false, "tree": []any{}},
	}
	files := map[string]string{"b1": immichCompose, "b2": "services: [", "b9": "services: {}"}

	mux := http.NewServeMux()
	for path, body := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(body)
		})
	}
	mux.HandleFunc("/api/v1/repos/alex/docker-compose-regis/git/blobs/{sha}", func(w http.ResponseWriter, r *http.Request) {
		blobs.Add(1)
		content := base64.StdEncoding.EncodeToString([]byte(files[r.PathValue("sha")]))
		json.NewEncoder(w).Encode(map[string]any{"encoding": "base64", "content": content})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestGiteaReadsComposeStacks: every stack directory of a
// docker-compose-<host> repo becomes a stack of that host, with its
// services' images, ports and labels; environment values are never kept.
func TestGiteaReadsComposeStacks(t *testing.T) {
	var blobs atomic.Int32
	srv := composeServer(t, &blobs)

	out, err := sources.GiteaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.GiteaDataset)
	if !d.StacksRead || len(d.Stacks) != 2 {
		t.Fatalf("stacks: %+v", d.Stacks)
	}

	broken, immich := d.Stacks[0], d.Stacks[1]
	if broken.Name != "broken" || !broken.Invalid || immich.Name != "immich" || immich.Host != "regis" || immich.Invalid {
		t.Fatalf("stacks: %+v", d.Stacks)
	}
	if immich.URL != "https://git.example/alex/docker-compose-regis/src/branch/main/immich/compose.yaml" {
		t.Fatalf("url: %s", immich.URL)
	}

	if len(immich.Services) != 2 {
		t.Fatalf("services: %+v", immich.Services)
	}
	db, server := immich.Services[0], immich.Services[1]
	if server.Name != "server" || server.Image != "ghcr.io/immich-app/immich-server:v1.120" || len(server.Ports) != 2 || server.Ports[0] != "2283:2283" || server.Ports[1] != "9000" || server.Labels["homepage.name"] != "Immich" {
		t.Fatalf("server: %+v", server)
	}
	if db.Ports[0] != "5433:5432/tcp" || db.Labels["backup"] != "true" {
		t.Fatalf("db: %+v", db)
	}

	if raw, _ := json.Marshal(d); strings.Contains(string(raw), "hunter2") {
		t.Fatal("environment value kept")
	}
}

// TestComposeBlobsReadOnce: an unchanged compose file (same blob) is not
// read again on the next fetch.
func TestComposeBlobsReadOnce(t *testing.T) {
	var blobs atomic.Int32
	srv := composeServer(t, &blobs)
	sctx := sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true}

	for range 2 {
		if _, err := sources.GiteaData.Fetch(context.Background(), sctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := blobs.Load(); n > 2 {
		t.Fatalf("blob reads: %d, want at most 2", n)
	}
}

// TestGiteaWithoutComposeRepos: a server whose search fails still
// answers; the stacks are marked unread, not empty.
func TestGiteaWithoutComposeRepos(t *testing.T) {
	srv := jsonServer(t, map[string]any{
		"/api/v1/user":                map[string]any{"login": "alex"},
		"/api/v1/user/repos":          []any{},
		"/api/v1/repos/issues/search": []any{},
	}, nil)

	out, err := sources.GiteaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "t", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if d := out.(*sources.GiteaDataset); d.StacksRead || len(d.Stacks) != 0 {
		t.Fatalf("stacks: %+v", d)
	}
}
