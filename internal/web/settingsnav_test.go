package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestSettingsSplit: own settings show only the own level, setup pages
// only the team and instance levels; "Einrichten" leads to the setup side.
func TestSettingsSplit(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	sideNav := regexp.MustCompile(`(?s)<nav class="settings-nav".*?</nav>`)

	own := sideNav.FindString(string(mustGet(t, srv, client, "/me/profile")))
	if !strings.Contains(own, `href="/me/security"`) || strings.Contains(own, `href="/admin/users"`) {
		t.Fatalf("own side:\n%s", own)
	}

	page := string(mustGet(t, srv, client, "/admin/users"))
	setup := sideNav.FindString(page)
	if strings.Contains(setup, `href="/me/security"`) || !strings.Contains(setup, `href="/admin/users" aria-current="page"`) {
		t.Fatalf("setup side:\n%s", setup)
	}
	menu := regexp.MustCompile(`(?s)<summary>Einrichten</summary>.*?</div>`).FindString(page)
	if !regexp.MustCompile(`<a href="/spaces/\d+/connections" aria-current="page">Einstellungen</a>`).MatchString(menu) {
		t.Fatalf("setup menu:\n%s", menu)
	}
}
