package web_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestImportDashyAndCodeView(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	form := mustGet(t, srv, client, "/import")
	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(form)[1]
	conf := "sections:\n  - name: Tools\n    items:\n      - {title: Git, url: \"https://git.lan\", icon: si-gitea}\n"
	resp := postFile(t, client, srv.URL+"/import", map[string]string{"csrf": csrf, "space_id": string(space), "kind": "dashy"}, "conf.yml", []byte(conf))
	body := readAll(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `import-preview`) || !strings.Contains(body, "1 Boards, 1 Kacheln") {
		t.Fatalf("dashy preview: %d\n%s", resp.StatusCode, body)
	}
	if strings.Contains(string(mustGet(t, srv, client, "/spaces/"+string(space)+"/code")), "si-gitea") {
		t.Fatal("preview already imported")
	}
	text := regexp.MustCompile(`(?s)<textarea name="text" hidden>(.*?)</textarea>`).FindStringSubmatch(body)
	if text == nil {
		t.Fatalf("preview carries no file to confirm:\n%s", body)
	}
	resp = postForm(t, client, srv.URL+"/import", url.Values{"csrf": {csrf}, "space_id": {string(space)}, "kind": {"dashy"},
		"text": {html.UnescapeString(text[1])}, "confirm": {"1"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashy import: %d", resp.StatusCode)
	}

	code2 := string(mustGet(t, srv, client, "/spaces/"+string(space)+"/code"))
	if !strings.Contains(code2, "Git") || !strings.Contains(code2, "si-gitea") {
		t.Fatalf("code view missing imported widget:\n%s", code2)
	}

	resp = postForm(t, client, srv.URL+"/spaces/"+string(space)+"/code", url.Values{"csrf": {csrf}, "text": {"boards: [\n bad"}, "mode": {"merge"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected broken YAML refused, got %d", resp.StatusCode)
	}

	export := string(mustGet(t, srv, client, "/spaces/"+string(space)+"/export"))
	if !strings.HasPrefix(export, "space: ") {
		t.Fatalf("unexpected export: %s", export)
	}
}
