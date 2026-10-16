package updates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/metrics"
	"andon/internal/outbound"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// The pull request changes the image as the compose file writes it:
// "nginx:1.25.0", though What's Up Docker says "library/nginx". Searched
// in that form, the line was never found and no request opened (seen
// against a throwaway Gitea).
func TestWriteKeepsComposeForm(t *testing.T) {
	const file = "services:\n  web:\n    image: nginx:1.25.0\n"
	var written string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/a/docker-compose-h", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"default_branch": "main"}`))
	})
	mux.HandleFunc("GET /api/v1/repos/a/docker-compose-h/contents/web/compose.yaml", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(file)), "sha": "s1"})
	})
	mux.HandleFunc("PUT /api/v1/repos/a/docker-compose-h/contents/web/compose.yaml", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Content string }
		json.NewDecoder(r.Body).Decode(&body)
		raw, _ := base64.StdEncoding.DecodeString(body.Content)
		written = string(raw)
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST /api/v1/repos/a/docker-compose-h/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"html_url": "http://gitea/a/docker-compose-h/pulls/1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &writer{to: outbound.Target{URL: srv.URL, Token: "t"}}
	stack := &sources.Stack{Host: "h", Name: "web", Repo: "a/docker-compose-h", Path: "web/compose.yaml"}
	it := Item{UpdateCase: metrics.UpdateCase{Key: "u-h-web-web", Image: "library/nginx", Written: "nginx", Tag: "1.25.0", NewTag: "1.31.7",
		Stack: stack, Service: "web", Pinned: true}, To: "1.31.7"}
	if _, err := w.Write(context.Background(), workbench.Planned[Item]{Item: it}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if written != "services:\n  web:\n    image: nginx:1.31.7\n" {
		t.Fatalf("file = %q", written)
	}
}
