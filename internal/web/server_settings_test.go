package web_test

import (
	"net/url"
	"strings"
	"testing"

	"andon/internal/settings"
)

// Admins set server settings in the UI; the analysis interval applies
// at once.
func TestServerSettingsForm(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	t.Cleanup(func() { settings.Publish(settings.Load()) })

	postForm(t, client, srv.URL+"/admin/settings/server", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "ANALYSIS_MINUTES": {"12"}, "APPRISE_API_URL": {"https://apprise.lan"},
	})
	page := string(mustGet(t, srv, client, "/admin/settings"))
	for _, want := range []string{`id="server"`, `name="ANALYSIS_MINUTES"`, `value="12"`, `value="https://apprise.lan"`, `name="ANTHROPIC_API_KEY"`} {
		if !strings.Contains(page, want) {
			t.Errorf("%q missing", want)
		}
	}
	if settings.Live(settings.Settings{}).AnalysisMinutes != 12 {
		t.Fatal("interval not applied")
	}
}
