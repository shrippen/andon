package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
)

// TestViewerSeesOnlyWhatIsAllowed: pages leave out what the viewer may
// not do (instance settings, Test and Edit of connections they cannot
// change, their own role as a choice); the server still refuses, now
// with a translated page that names the reason.
func TestViewerSeesOnlyWhatIsAllowed(t *testing.T) {
	srv, admin, code := newTestServer(t)
	setupAdmin(t, srv, admin, code)
	login(t, srv, admin)
	space := instanceSpace(t, srv, admin)
	conn := createConnection(t, srv.URL, admin, csrfToken(t, srv, admin), space, "kimai", "demo://kimai")

	// A plain user, accepted by invitation.
	user := freshClient(t)
	postForm(t, user, srv.URL+inviteLink(t, srv, admin, "user@x.de"), url.Values{"name": {"User"}, "password": {"user-password-long"}, "locale": {"de"}})

	test, edit := `action="/connections/`+conn+`/test"`, `href="/connections/`+conn+`?tab=settings"`
	if page := string(mustGet(t, srv, admin, "/spaces/"+space+"/connections")); !strings.Contains(page, test) || !strings.Contains(page, edit) {
		t.Fatalf("the admin misses Test or Edit:\n%s", page)
	}
	if page := string(mustGet(t, srv, user, "/connections")); strings.Contains(page, test) || strings.Contains(page, edit) {
		t.Fatalf("the user is offered Test or Edit of the instance's connection:\n%s", page)
	}
	if record := string(mustGet(t, srv, user, "/connections/"+conn)); strings.Contains(record, test) {
		t.Fatalf("the record offers the user a test:\n%s", record)
	}

	// Instance settings are not the user's: refused with the reason, in
	// the app frame, in German.
	denied := i18n.T("error.denied", enums.LocaleDE, nil)
	for _, path := range []string{"/spaces/" + space + "/settings/page", "/spaces/" + space + "/connections", "/admin/settings"} {
		status, body := browse(t, user, http.MethodGet, srv.URL+path, nil, nil)
		if status != http.StatusForbidden || !strings.Contains(body, pageMarker) || !strings.Contains(body, denied) {
			t.Errorf("%s: %d, want a framed 403 naming the reason:\n%s", path, status, body)
		}
	}

	// The server still refuses, also a soft page change (htmx boost).
	csrf := csrfToken(t, srv, user)
	boost := map[string]string{"HX-Request": "true", "HX-Boosted": "true"}
	for _, path := range []string{"/spaces/" + space + "/settings/page", "/connections/" + conn + "/delete"} {
		status, body := browse(t, user, http.MethodPost, srv.URL+path, url.Values{"csrf": {csrf}, "title": {"Mine"}}, boost)
		if status != http.StatusForbidden || !strings.Contains(body, denied) || strings.Contains(body, "access denied") {
			t.Errorf("POST %s: %d, want a translated 403:\n%s", path, status, body)
		}
	}

	// The own role is text; the other user's a choice.
	users := string(mustGet(t, srv, admin, "/admin/users"))
	forms := regexp.MustCompile(`/admin/users/(\d+)/role`).FindAllStringSubmatch(users, -1)
	if len(forms) != 1 {
		t.Fatalf("expected a role form only for the other user, got %d:\n%s", len(forms), users)
	}
}
