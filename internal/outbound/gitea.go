package outbound

// Writes of the update bench: a change to one file of a Gitea repo,
// proposed as a pull request on a new branch.
//
//	GET  repos/{repo}                    default branch
//	GET  repos/{repo}/contents/{path}    content and sha
//	PUT  repos/{repo}/contents/{path}    new content on a new branch
//	POST repos/{repo}/pulls              branch → default branch

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"

	"andon/internal/drivers/services"
)

// GiteaChange is one file changed by a function of its content; Change
// returns the new content, or an error when the file does not hold what
// it expects.
type GiteaChange struct {
	Repo, Path     string // "alex/docker-compose-regis", "immich/compose.yaml"
	Branch         string // new branch, "andon/immich-1.132.3"
	Message, Title string
	Body           string
	Change         func(old []byte) ([]byte, error)
}

func gitea(to Target) services.GiteaApi {
	return services.GiteaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
}

// GiteaPropose writes the change on a new branch and opens a pull
// request into the default branch; it returns the request's page.
func GiteaPropose(ctx context.Context, to Target, c GiteaChange) (string, error) {
	api := gitea(to)
	repo, err := api.Get(ctx, "repos/"+c.Repo, nil)
	if err != nil {
		return "", err
	}
	base, _ := repo.(map[string]any)["default_branch"].(string)

	path := "repos/" + c.Repo + "/contents/" + escapePath(c.Path)
	file, err := api.Get(ctx, path, url.Values{"ref": {base}})
	if err != nil {
		return "", err
	}
	m, _ := file.(map[string]any)
	encoded, _ := m["content"].(string)
	sha, _ := m["sha"].(string)
	old, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	content, err := c.Change(old)
	if err != nil {
		return "", err
	}

	if _, err := api.Send(ctx, http.MethodPut, path, map[string]any{"content": base64.StdEncoding.EncodeToString(content), "sha": sha,
		"branch": base, "new_branch": c.Branch, "message": c.Message}); err != nil {
		return "", err
	}
	pr, err := api.Send(ctx, http.MethodPost, "repos/"+c.Repo+"/pulls", map[string]any{"head": c.Branch, "base": base, "title": c.Title, "body": c.Body})
	if err != nil {
		return "", err
	}
	page, _ := pr.(map[string]any)["html_url"].(string)
	if page == "" {
		return "", errors.New("gitea: no pull request in the answer")
	}
	return page, nil
}

// escapePath escapes each segment of a repo path.
func escapePath(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}
