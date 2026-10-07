package web_test

// From a connection to a tile on a board (ROADMAP "Reibung" #01) and the
// new-connection flow (#02): library "Auf Board legen", empty board tips,
// tiles offered after a green test, explained test errors, the test after
// a new token, the grouped service picker.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// startBoard opens the start board and returns its path and id.
func startBoard(t *testing.T, srv *httptest.Server, client *http.Client) (string, string) {
	t.Helper()
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	path := resp.Request.URL.Path
	return path, boardIDFrom(path)
}

// newConn creates a shared Kimai connection at target and returns its id.
func newConn(t *testing.T, srv *httptest.Server, client *http.Client, name, target string) string {
	t.Helper()
	form := string(mustGet(t, srv, client, "/connections/new?service=kimai"))
	space := regexp.MustCompile(`<option value="(\d+)">`).FindStringSubmatch(form)[1]
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := postForm(t, &noFollow, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"kimai"}, "name": {name}, "url": {target}, "mode": {"shared"}, "secret": {"tok"}, "tls": {"verify"}})
	return regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]
}

// kimaiUp answers Kimai's version check.
func kimaiUp(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version": "2.30.0"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestLibraryPutOnBoard: every library row offers "Auf Board legen" with
// the boards the caller may edit; it places the tile there.
func TestLibraryPutOnBoard(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	boardPath, board := startBoard(t, srv, client)
	space := regexp.MustCompile(`space=(\d+)`).FindStringSubmatch(string(mustGet(t, srv, client, "/widgets/new?dialog")))[1]
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"type": {"note"}, "title": {"Lib Note"}, "cfg.text": {"hi"}})

	lib := string(mustGet(t, srv, client, "/widgets"))
	if !strings.Contains(lib, `action="/widgets/{widget}/board"`) || !strings.Contains(lib, `<option value="`+board+`">`) ||
		!strings.Contains(lib, "Auf Board legen") {
		t.Fatalf("no put-on-board action:\n%s", lib)
	}
	widget := regexp.MustCompile(`data-widget="(\d+)"`).FindStringSubmatch(lib)[1]

	resp := postForm(t, client, srv.URL+"/widgets/"+widget+"/board", url.Values{"csrf": {csrfToken(t, srv, client)}, "board_id": {board}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != boardPath {
		t.Fatalf("answer %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if page := string(mustGet(t, srv, client, boardPath)); !strings.Contains(page, "Lib Note") {
		t.Fatalf("tile not on the board:\n%s", page)
	}
}

// TestEmptyBoardTips: an empty board says so, offers the gallery and a
// starter for each connection without a tile; one click puts it there.
func TestEmptyBoardTips(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	conn := newConn(t, srv, client, "Zeiten", "http://127.0.0.1:1")
	boardPath, board := startBoard(t, srv, client)

	page := string(mustGet(t, srv, client, boardPath))
	if !strings.Contains(page, "Dieses Board ist leer") || !regexp.MustCompile(`href="/widgets/new\?space=\d+&amp;section=\d+`).MatchString(page) ||
		!strings.Contains(page, `name="type" value="kimai_week"`) || !strings.Contains(page, `name="conn_id" value="`+conn+`"`) ||
		!strings.Contains(page, "Zeiten") {
		t.Fatalf("no empty state with tips:\n%s", page)
	}

	resp := postForm(t, client, srv.URL+"/boards/tile", url.Values{"csrf": {csrfToken(t, srv, client)}, "board_id": {board},
		"type": {"kimai_week"}, "conn_id": {conn}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != boardPath {
		t.Fatalf("answer %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page = string(mustGet(t, srv, client, boardPath))
	if strings.Contains(page, "Dieses Board ist leer") || !strings.Contains(page, "w-kimai_week") {
		t.Fatalf("tile not placed:\n%s", page)
	}
}

// TestGreenTestOffersTiles: after a green test on the record the answer
// carries the service's templates with a board choice; a failed test
// none. The record page shows them too.
func TestGreenTestOffersTiles(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	_, board := startBoard(t, srv, client)
	up := newConn(t, srv, client, "K", kimaiUp(t).URL)
	down := newConn(t, srv, client, "D", "http://127.0.0.1:1")

	record := string(mustGet(t, srv, client, "/connections/"+up))
	if !strings.Contains(record, `id="conn-tiles"`) || !strings.Contains(record, `name="suggest" value="1"`) {
		t.Fatalf("record lacks the slot for offered tiles:\n%s", record)
	}

	res, err := client.PostForm(srv.URL+"/connections/"+up+"/check", url.Values{"csrf": {csrfToken(t, srv, client)}, "suggest": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if !strings.Contains(body, `id="conn-tiles" hx-swap-oob="true"`) || !strings.Contains(body, `action="/boards/tile"`) ||
		!strings.Contains(body, `formaction="/boards/tile?type=kimai_week&conn_id=`+up+`"`) ||
		!strings.Contains(body, `name="board_id" value="`+board+`"`) || !strings.Contains(body, "Kimai-Woche auf Start legen") {
		t.Fatalf("green test offers no tiles:\n%s", body)
	}

	res, err = client.PostForm(srv.URL+"/connections/"+down+"/check", url.Values{"csrf": {csrfToken(t, srv, client)}, "suggest": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, res); strings.Contains(body, `action="/boards/tile"`) {
		t.Fatalf("failed test offers tiles:\n%s", body)
	}
}

// TestTestErrorExplained: a refused connection reads as a translated cause
// with the raw text as detail; a blocked host points admins to the
// network settings.
func TestTestErrorExplained(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	refused := newConn(t, srv, client, "R", "http://127.0.0.1:1")
	blocked := newConn(t, srv, client, "B", "http://169.254.169.254:1")

	res, err := client.PostForm(srv.URL+"/connections/"+refused+"/check", url.Values{"csrf": {csrfToken(t, srv, client)}})
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if !strings.Contains(body, "Verbindung abgelehnt") || !strings.Contains(body, "connection refused: 127.0.0.1") {
		t.Fatalf("refusal not explained:\n%s", body)
	}

	page := string(mustGet(t, srv, client, "/connections/"+blocked+"?welcome"))
	if !strings.Contains(page, "Netzwerk-Regel") || !strings.Contains(page, `href="/admin/settings#network"`) ||
		!strings.Contains(page, "egress denied") {
		t.Fatalf("blocked host not explained:\n%s", page)
	}
	if admin := string(mustGet(t, srv, client, "/admin/settings")); !strings.Contains(admin, `id="network"`) {
		t.Fatal("admin settings lack the network anchor")
	}
}

// TestNewTokenRunsTest: saving a new token on the record runs the test
// and shows its result.
func TestNewTokenRunsTest(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	id := newConn(t, srv, client, "K", kimaiUp(t).URL)

	resp := postForm(t, client, srv.URL+"/connections/"+id+"/secret", url.Values{"csrf": {csrfToken(t, srv, client)}, "secret": {"neu"}})
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "tested") {
		t.Fatalf("answer %d to %q", resp.StatusCode, loc)
	}
	if page := string(mustGet(t, srv, client, loc)); !strings.Contains(page, "✓ OK") {
		t.Fatalf("no test result after the new token:\n%s", page)
	}
}

// TestServicePickGrouped: the service picker has a search and groups the
// services by topic, like the tile gallery.
func TestServicePickGrouped(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	pick := string(mustGet(t, srv, client, "/connections/new"))
	if !strings.Contains(pick, `id="gal-q"`) || !strings.Contains(pick, `class="gal-nav"`) ||
		strings.Count(pick, `class="gal-group"`) < 5 || !strings.Contains(pick, `<p class="gal-none muted" hidden>`) {
		t.Fatalf("picker without search or groups:\n%s", pick)
	}
	homelab := regexp.MustCompile(`(?s)id="topic-homelab".*?</section>`).FindString(pick)
	if !strings.Contains(homelab, "service=scrutiny") || strings.Contains(homelab, "service=kimai") {
		t.Fatalf("homelab group:\n%s", homelab)
	}
}

// TestOwnLoginRunsTest: activating a template with one's own login runs
// the test and the connections page shows its result.
func TestOwnLoginRunsTest(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "space_id": {instanceSpace(t, srv, client)}, "service": {"kimai"},
		"name": {"Mine"}, "url": {kimaiUp(t).URL}, "mode": {"personal"}, "tls": {"verify"}})
	conn := regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1]

	resp = postForm(t, client, srv.URL+"/connections/"+conn+"/activate", url.Values{"csrf": {csrf}, "secret": {"tok"}})
	page := readBody(t, getFollowingRedirect(t, srv, client, resp.Header.Get("Location")))
	if !strings.Contains(page, "<b>Mine</b> ✓ OK") {
		t.Fatalf("no test after the own login:\n%s", page)
	}
}
