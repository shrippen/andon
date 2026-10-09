package web_test

import (
	"io"
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

// TestConnectionMove: another server in the settings form saves the
// rest and leads to the move page; the move asks whether the token goes
// along, tests the new address and keeps the connection (same id).
func TestConnectionMove(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"kimai"}, "space_id": {string(space)},
		"name": {"Kimai"}, "url": {"demo://kimai"}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	id := regexp.MustCompile(`id="health-(\d+)"`).FindStringSubmatch(string(mustGet(t, srv, client, "/connections")))[1]

	res := postForm(t, client, srv.URL+"/connections/"+id+"/edit", url.Values{"csrf": {csrf}, "name": {"Zeit"}, "url": {"demo://kimai2"},
		"mode": {"shared"}, "tls": {"verify"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/connections/"+id+"/move?url=demo%3A%2F%2Fkimai2" {
		t.Fatalf("edit to another server: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	record := string(mustGet(t, srv, client, "/connections/"+id))
	if !strings.Contains(record, "Zeit") || !strings.Contains(record, "demo://kimai<") {
		t.Fatal("edit saved the new server or lost the name")
	}

	page := string(mustGet(t, srv, client, res.Header.Get("Location")))
	if !strings.Contains(page, `value="demo://kimai2"`) || !strings.Contains(page, `name="keep" value="keep"`) {
		t.Fatalf("move page:\n%s", page)
	}

	// A failing test saves nothing and offers to move untested.
	failed := postForm2(t, client, srv.URL+"/connections/"+id+"/move", url.Values{"csrf": {csrf}, "url": {"http://127.0.0.1:1"}, "keep": {"keep"}})
	body, _ := io.ReadAll(failed.Body)
	failed.Body.Close()
	if failed.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `name="untested"`) {
		t.Fatalf("failed test: %d\n%s", failed.StatusCode, body)
	}

	res = postForm(t, client, srv.URL+"/connections/"+id+"/move", url.Values{"csrf": {csrf}, "url": {"demo://kimai2"}, "keep": {"keep"}})
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/connections/"+id) {
		t.Fatalf("move: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if !strings.Contains(string(mustGet(t, srv, client, "/connections/"+id)), "demo://kimai2") {
		t.Fatal("not moved")
	}
}

// TestConnectionAdopt: the settings tab offers the other connections of
// the service; taking one over deletes it and says what to check.
func TestConnectionAdopt(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, name := range []string{"Alt", "Neu"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {"kimai"}, "space_id": {string(space)},
			"name": {name}, "url": {"demo://" + strings.ToLower(name)}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	ids := regexp.MustCompile(`id="health-(\d+)"`).FindAllStringSubmatch(string(mustGet(t, srv, client, "/connections")), -1)
	if len(ids) != 2 {
		t.Fatalf("connections: %v", ids)
	}
	old, fresh := ids[0][1], ids[1][1]

	settings := string(mustGet(t, srv, client, "/connections/"+fresh+"?tab=settings"))
	if !strings.Contains(settings, `action="/connections/`+fresh+`/adopt"`) || !strings.Contains(settings, `<option value="`+old+`">`) {
		t.Fatalf("no take-over offered:\n%s", settings)
	}

	res := postForm(t, client, srv.URL+"/connections/"+fresh+"/adopt", url.Values{"csrf": {csrf}, "from": {old}})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "adopted") {
		t.Fatalf("adopt: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	gone, err := client.Get(srv.URL + "/connections/" + old)
	if err != nil {
		t.Fatal(err)
	}
	gone.Body.Close()
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("old connection still there: %d", gone.StatusCode)
	}
}
