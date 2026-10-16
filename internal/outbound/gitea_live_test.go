package outbound_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// kindRepo is a Gitea repository a test created, in the write log.
const kindRepo = "repo"

// A pull request with a new image tag on a repository of its own: the
// write of the update bench (services/updates); the repository is
// deleted at the end. Never against the own Gitea: it holds the real
// compose files (live.Target).
func TestGiteaProposeLive(t *testing.T) {
	to := live.Target(t, live.Gitea)
	api := services.GiteaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	ctx := context.Background()
	name := strings.NewReplacer(" ", "-", ":", "").Replace(strings.ToLower(live.Name(t)))

	created, err := api.Send(ctx, http.MethodPost, "user/repos", map[string]any{"name": name, "auto_init": true, "default_branch": "main"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	repo, _ := asObject(created)["full_name"].(string)
	live.Created(t, live.Gitea, kindRepo, repo)
	t.Cleanup(func() {
		live.Change(t, live.Gitea, live.Delete, kindRepo, repo)
		if _, err := api.Send(ctx, http.MethodDelete, "repos/"+repo, nil); err != nil {
			t.Errorf("delete repo: %v", err)
		}
	})

	const path, before, after = "immich/compose.yaml", "image: ghcr.io/immich-app/immich-server:v1.131.0\n", "image: ghcr.io/immich-app/immich-server:v1.132.3\n"
	live.Change(t, live.Gitea, live.Update, kindRepo, repo)
	if _, err := api.Send(ctx, http.MethodPost, "repos/"+repo+"/contents/"+path, map[string]any{
		"content": base64.StdEncoding.EncodeToString([]byte(before)), "message": "compose"}); err != nil {
		t.Fatalf("add file: %v", err)
	}

	branch := fmt.Sprintf("andon/immich-%d", time.Now().Unix())
	live.Change(t, live.Gitea, live.Update, kindRepo, repo)
	page, err := outbound.GiteaPropose(ctx, to, outbound.GiteaChange{Repo: repo, Path: path, Branch: branch,
		Message: "Update immich", Title: "Update immich to v1.132.3", Body: name,
		Change: func(old []byte) ([]byte, error) { return bytes.Replace(old, []byte("v1.131.0"), []byte("v1.132.3"), 1), nil }})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !strings.Contains(page, repo+"/pulls/") {
		t.Errorf("page = %q", page)
	}

	pulls, err := api.Get(ctx, "repos/"+repo+"/pulls", url.Values{"state": {"open"}})
	if err != nil {
		t.Fatalf("pulls: %v", err)
	}
	list, _ := pulls.([]any)
	if len(list) != 1 || asObject(asObject(list[0])["head"])["ref"] != branch || asObject(asObject(list[0])["base"])["ref"] != "main" {
		t.Errorf("pulls: %v", pulls)
	}
	file, err := api.Get(ctx, "repos/"+repo+"/contents/"+path, url.Values{"ref": {branch}})
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content, _ := base64.StdEncoding.DecodeString(fmt.Sprint(asObject(file)["content"]))
	if string(content) != after {
		t.Errorf("file on %s = %q, want %q", branch, content, after)
	}
}
