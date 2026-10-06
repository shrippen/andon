package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestConnectionTestNamesGaps: a Kimai whose mileage plugin is read-only
// passes the test with a note on each part it lacks, in the toast and
// on the page.
func TestConnectionTestNamesGaps(t *testing.T) {
	kimai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/mileage/ping":
			w.Write([]byte(`{"permissions": {"view": true, "editOwn": false}, "features": ["placesWrite"]}`))
		case "/api/users/me":
			w.Write([]byte(`{"preferences": [{"name": "work_monday", "value": 28800}]}`))
		default:
			w.Write([]byte(`{"version": "2.30.0"}`))
		}
	}))
	defer kimai.Close()

	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := regexp.MustCompile(`<option value="(\d+)">`).FindStringSubmatch(string(mustGet(t, srv, client, "/connections/new?service=kimai")))[1]
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := postForm(t, &noFollow, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space}, "service": {"kimai"},
		"name": {"K"}, "url": {kimai.URL}, "mode": {"shared"}, "secret": {"tok"}, "tls": {"verify"}})
	id := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]

	res, err := client.PostForm(srv.URL+"/connections/"+id+"/check", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if !strings.Contains(body, `data-kind="warn"`) || strings.Count(body, "Recht editOwn fehlt") != 4 {
		t.Fatalf("toast:\n%s", body)
	}
	if strings.Contains(body, "Sollarbeitszeit") {
		t.Fatalf("contract reported missing:\n%s", body)
	}
	res, err = client.PostForm(srv.URL+"/connections/"+id+"/test", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, res); strings.Count(body, "Recht editOwn fehlt") < 4 || !strings.Contains(body, "Orte ändern (Zuhause, Arbeit, Kunde, Sonstiges)") {
		t.Fatalf("page:\n%s", body)
	}
}
