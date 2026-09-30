package web_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func postFile(t *testing.T, client *http.Client, target string, fields map[string]string, name string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	part, _ := w.CreateFormFile("file", name)
	_, _ = part.Write(data)
	_ = w.Close()
	resp, err := client.Post(target, w.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return resp
}

// TestThemeDuplicateEditFontExportImport: duplicate Kante, change a
// token, upload a font, and round-trip it through the ZIP export.
func TestThemeDuplicateEditFontExportImport(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	list := mustGet(t, srv, client, "/themes")
	builtin := regexp.MustCompile(`name="theme_id" value="(\d+)"`).FindSubmatch(list)
	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(list)
	if builtin == nil || space == nil {
		t.Fatalf("theme list incomplete:\n%s", list)
	}

	resp := postForm(t, client, srv.URL+"/themes/duplicate", url.Values{
		"csrf": {csrf}, "theme_id": {string(builtin[1])}, "space_id": {string(space[1])}, "name": {"Mine"},
	})
	edit := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || edit == "" {
		t.Fatalf("duplicate: %d", resp.StatusCode)
	}
	id := strings.TrimPrefix(edit, "/themes/")

	resp = postForm(t, client, srv.URL+edit, url.Values{"csrf": {csrf}, "name": {"Mine"}, "dark--accent": {"#123456"}})
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		t.Fatalf("save: %d", resp.StatusCode)
	}

	resp = postFile(t, client, srv.URL+edit+"/fonts", map[string]string{"csrf": csrf}, "../Evil.woff2", []byte("x"))
	resp.Body.Close()
	resp = postFile(t, client, srv.URL+edit+"/fonts", map[string]string{"csrf": csrf}, "Inter-600.woff2", []byte("wOF2font"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("font upload: %d", resp.StatusCode)
	}

	css := string(mustGet(t, srv, client, "/theme/"+id+".css"))
	if !strings.Contains(css, "--accent:#123456") || !strings.Contains(css, `font-family:"Inter";font-weight:600`) {
		t.Fatalf("css missing token or font-face:\n%s", css)
	}
	if font := mustGet(t, srv, freshClient(t), "/theme-fonts/"+id+"/Inter-600.woff2"); string(font) != "wOF2font" {
		t.Fatalf("font not served: %q", font)
	}

	zipData := mustGet(t, srv, client, edit+"/export")
	resp = postFile(t, client, srv.URL+"/themes/import", map[string]string{"csrf": csrf, "space_id": string(space[1])}, "mine.zip", zipData)
	resp.Body.Close()
	imported := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || imported == edit {
		t.Fatalf("import: %d %s", resp.StatusCode, imported)
	}
	css = string(mustGet(t, srv, client, "/theme/"+strings.TrimPrefix(imported, "/themes/")+".css"))
	if !strings.Contains(css, "--accent:#123456") || !strings.Contains(css, "Inter-600.woff2") {
		t.Fatalf("import lost token or font:\n%s", css)
	}

	if !strings.Contains(string(mustGet(t, srv, client, "/styleguide")), `class="theme-sample"`) {
		t.Fatal("styleguide missing sample")
	}
}

func TestThemeFromPreset(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	list := mustGet(t, srv, client, "/themes")
	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(list)
	if space == nil || !strings.Contains(string(list), `<option value="dracula">`) {
		t.Fatalf("preset form missing:\n%s", list)
	}
	resp := postForm(t, client, srv.URL+"/themes/preset", url.Values{"csrf": {csrf}, "preset": {"dracula"}, "space_id": {string(space[1])}})
	edit := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(edit, "/themes/") {
		t.Fatalf("preset: %d", resp.StatusCode)
	}
	if page := string(mustGet(t, srv, client, edit)); !strings.Contains(page, "Dracula") || !strings.Contains(page, "#282a36") {
		t.Fatal("preset theme lacks its name or colors")
	}

	resp = postForm(t, client, srv.URL+"/themes/preset", url.Values{"csrf": {csrf}, "preset": {"nope"}, "space_id": {string(space[1])}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown preset: %d", resp.StatusCode)
	}
}

// TestProfileTheme: everyone picks their own theme in the profile; it wins
// over the instance default, "Standard" goes back to it, and a theme one
// may not use is refused.
func TestProfileTheme(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	list := mustGet(t, srv, client, "/themes")
	space := regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(list)
	resp := postForm(t, client, srv.URL+"/themes/preset", url.Values{"csrf": {csrf}, "preset": {"dracula"}, "space_id": {string(space[1])}})
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/themes/")

	save := func(theme string) int {
		r := postForm(t, client, srv.URL+"/me/profile", url.Values{"csrf": {csrf}, "name": {"Admin"}, "locale": {"de"}, "color_mode": {"auto"}, "theme_id": {theme}})
		return r.StatusCode
	}
	themeLink := regexp.MustCompile(`href="/theme/(\d+)\.css`)

	if got := save(id); got != http.StatusSeeOther {
		t.Fatalf("save theme: %d", got)
	}
	profile := mustGet(t, srv, client, "/me/profile")
	if m := themeLink.FindSubmatch(profile); m == nil || string(m[1]) != id {
		t.Fatalf("personal theme not applied: %s", m)
	}
	if !strings.Contains(string(profile), `<option value="`+id+`" selected>`) {
		t.Fatal("profile does not show the chosen theme")
	}
	if list := string(mustGet(t, srv, client, "/themes")); !regexp.MustCompile(`Dracula[\s\S]{0,300}Aktiv[\s\S]{0,200}Dein Theme`).MatchString(list) ||
		!strings.Contains(list, "Instanz-Standard") {
		t.Fatalf("theme list does not mark the active, own and default theme:\n%s", list)
	}

	if got := save(""); got != http.StatusSeeOther {
		t.Fatalf("reset theme: %d", got)
	}
	if m := themeLink.FindSubmatch(mustGet(t, srv, client, "/me/profile")); m == nil || string(m[1]) == id {
		t.Fatalf("theme not reset: %s", m)
	}

	if got := save("99999"); got != http.StatusBadRequest {
		t.Fatalf("unknown theme accepted: %d", got)
	}
}
