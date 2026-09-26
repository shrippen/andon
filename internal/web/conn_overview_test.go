package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestConnectionsOverviewStates: the list opens with a count per state,
// every row has a state cell, and a test answers with that cell to swap
// in, so a good test ends "failing" at once.
func TestConnectionsOverviewStates(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"kimai"}, "space_id": {string(space)},
		"name": {"Kimai"}, "url": {"demo://kimai"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})

	list := string(mustGet(t, srv, client, "/connections"))
	id := regexp.MustCompile(`id="health-(\d+)"`).FindStringSubmatch(list)
	if !strings.Contains(list, `class="conn-summary"`) || id == nil {
		t.Fatalf("overview lacks summary or state cell:\n%s", list)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/connections/"+id[1]+"/check", strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	res.Body.Close()
	if !strings.Contains(body, `id="health-`+id[1]+`" data-state="ok" hx-swap-oob="true"`) {
		t.Fatalf("check does not swap in the new state:\n%s", body)
	}
}

// TestConnectionAdvancedSavesWithForm: expiry, daily budget and YAML
// options save with the one "Speichern" of the edit form.
func TestConnectionAdvancedSavesWithForm(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"kimai"}, "space_id": {string(space)},
		"name": {"Kimai"}, "url": {"demo://kimai"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	id := regexp.MustCompile(`id="health-(\d+)"`).FindStringSubmatch(string(mustGet(t, srv, client, "/connections")))[1]

	res := postForm(t, client, srv.URL+"/connections/"+id+"/edit", url.Values{"csrf": {csrf}, "name": {"Kimai"}, "url": {"demo://kimai"},
		"mode": {"shared"}, "tls": {"verify"}, "expires": {"2027-01-31"}, "budget": {"40"}, "options_yaml": {"week_hours: 35\n"}, "options_before": {""}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d", res.StatusCode)
	}
	edit := string(mustGet(t, srv, client, "/connections/"+id+"/edit"))
	for _, want := range []string{`value="2027-01-31"`, `name="budget" type="number" min="0" value="40"`, "week_hours: 35"} {
		if !strings.Contains(edit, want) {
			t.Fatalf("edit page lacks %q after save:\n%s", want, edit)
		}
	}
}
