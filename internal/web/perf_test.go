package web_test

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/db"
)

// editTiles is the board size the edit mode must stay fast at.
const editTiles = 50

// Allocation budgets, in bytes: deterministic, unlike wall time. Cloning
// the whole template set per request cost ~3.7 MB per fragment.
const (
	editPageBudget = 8 << 20
	fragmentBudget = 1 << 20
)

var (
	addLinkRe  = regexp.MustCompile(`/widgets/new\?space=\d+&(?:amp;)?section=(\d+)&(?:amp;)?board=\d+&(?:amp;)?version=(\d+)`)
	fragmentRe = regexp.MustCompile(`hx-get="(/widget-fragments/\d+)"`)
)

// TestEditBoardFiftyLinks: a board with 50 link tiles in edit mode, and
// each tile's status fragment, stay within an allocation budget.
func TestEditBoardFiftyLinks(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path

	// Quick links: one POST per tile, each bumps the board version.
	for i := range editTiles {
		add := addLinkRe.FindSubmatch(mustGet(t, srv, client, boardURL+"?edit"))
		postForm(t, client, srv.URL+"/sections/"+string(add[1])+"/quick-link", url.Values{"csrf": {csrf},
			"version": {string(add[2])}, "board_id": {boardIDFrom(boardURL)}, "url": {fmt.Sprintf("https://s%d.example", i)}})
	}

	var page []byte
	pageBytes := allocated(func() { page = mustGet(t, srv, client, boardURL+"?edit") })
	// Hidden inputs per tile were most of the page; elements tied to a
	// form elsewhere (form="…") are attached one by one on every swap,
	// quadratic in their number: 5 s of a 240-tile swap on a slow device.
	if hidden := bytes.Count(page, []byte(`type="hidden"`)); hidden > 4*editTiles {
		t.Errorf("edit page has %d hidden inputs", hidden)
	}
	if linked := bytes.Count(page, []byte(` form="`)); linked > editTiles {
		t.Errorf("edit page has %d elements with a form attribute", linked)
	}
	if icons := bytes.Count(page, []byte("<svg")); icons >= editTiles {
		t.Errorf("edit page has %d SVG icons: each costs a shadow tree", icons)
	}
	frags := fragmentRe.FindAllSubmatch(page, -1)
	if len(frags) != editTiles {
		t.Fatalf("expected %d fragments, got %d", editTiles, len(frags))
	}
	fragBytes := allocated(func() {
		for _, f := range frags {
			fetchOK(t, srv, client, string(f[1]))
		}
	}) / editTiles

	// Known link states render with the page: no request per tile.
	if loads := bytes.Count(mustGet(t, srv, client, boardURL+"?edit"), []byte(`hx-trigger="load`)); loads > 0 {
		t.Errorf("edit page still loads %d tiles one by one", loads)
	}

	t.Logf("edit page %d KB, fragment %d KB", pageBytes>>10, fragBytes>>10)
	if pageBytes > editPageBudget {
		t.Errorf("edit page allocates %d KB, budget %d KB", pageBytes>>10, editPageBudget>>10)
	}
	if fragBytes > fragmentBudget {
		t.Errorf("fragment allocates %d KB, budget %d KB", fragBytes>>10, fragmentBudget>>10)
	}
}

// allocated is how many heap bytes run allocates (server and client share
// the process).
func allocated(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func fetchOK(t *testing.T, srv *httptest.Server, client *http.Client, path string) {
	t.Helper()
	body := mustGet(t, srv, client, path)
	if len(body) == 0 {
		t.Fatalf("%s: empty", path)
	}
}

// TestFreshCardSkipsLoad: a feed tile whose stored feed is within the
// source's TTL renders with the page and fetches nothing on load; the
// load would only swap in the same HTML and re-lay out the board.
func TestFreshCardSkipsLoad(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<rss version="2.0"><channel><title>T</title><item><title>First post</title></item></channel></rss>`))
	}))
	defer feed.Close()

	space := string(regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new"))[1])
	postForm(t, client, srv.URL+"/widgets", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space}, "type": {"rss"},
		"title": {"News"}, "cfg.url": {feed.URL}})
	boardURL, section, version, widget := placeTarget(t, srv, client, "News")
	postForm(t, client, srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections/"+section+"/place", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "widget_id": {widget}, "version": {version}})

	// The first view fetches the feed.
	frag := fragmentRe.FindSubmatch(mustGet(t, srv, client, boardURL))
	if frag == nil {
		t.Fatal("no fragment on the board")
	}
	fetchOK(t, srv, client, string(frag[1]))

	page := mustGet(t, srv, client, boardURL)
	if !bytes.Contains(page, []byte("First post")) {
		t.Fatal("feed not rendered with the page")
	}
	if bytes.Contains(page, []byte(`hx-trigger="load`)) {
		t.Fatal("fresh feed tile loads again")
	}
}

// lockWait bounds a page render next to a writer; one that needed the
// write lock would wait out the busy timeout (5 s) per transaction.
const lockWait = time.Second

// TestPageSkipsWriteLock: a board page only reads, so it renders while a
// background job holds the database's write lock.
func TestPageSkipsWriteLock(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path
	mustGet(t, srv, client, boardURL)

	raw, _ := testDBs.Load(srv.URL)
	locked, release := make(chan struct{}), make(chan struct{})
	go db.WithTx(raw.(*sql.DB), func(*sql.Tx) error {
		close(locked)
		<-release
		return nil
	})
	<-locked
	defer close(release)

	start := time.Now()
	page := mustGet(t, srv, client, boardURL+"?edit")
	if took := time.Since(start); took >= lockWait || !bytes.Contains(page, []byte("tile-")) {
		t.Fatalf("page waited %v for the write lock", took)
	}
}

// TestTileActionAnswersSection: a tile strip action sent by htmx for its
// section gets only that section back, plus the board's new version; the
// whole page would cost seconds to swap on a big board.
func TestTileActionAnswersSection(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path
	add := addLinkRe.FindSubmatch(mustGet(t, srv, client, boardURL+"?edit"))
	postForm(t, client, srv.URL+"/sections/"+string(add[1])+"/quick-link", url.Values{"csrf": {csrf},
		"version": {string(add[2])}, "board_id": {boardIDFrom(boardURL)}, "url": {"https://a.example"}})

	page := mustGet(t, srv, client, boardURL+"?edit")
	placement := regexp.MustCompile(`/placements/(\d+)/rows`).FindSubmatch(page)[1]
	section := regexp.MustCompile(`id="(dsec-\d+)"`).FindSubmatch(page)[1]
	version := regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(page)[1]

	form := url.Values{"csrf": {csrf}, "board_id": {boardIDFrom(boardURL)}, "version": {string(version)}, "rows": {"2"}}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/placements/"+string(placement)+"/rows", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", string(section))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(body), `<section`) || strings.Contains(body, "<html") {
		t.Fatalf("expected the section alone, got %d:\n%.300s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `data-placement="`+string(placement)+`" data-rows="2"`) {
		t.Fatalf("section lacks the change:\n%s", body)
	}
	next, _ := strconv.Atoi(string(version))
	if trigger := resp.Header.Get("HX-Trigger"); !strings.Contains(trigger, `"boardVersion":`+strconv.Itoa(next+1)) {
		t.Fatalf("no new version in HX-Trigger: %q", trigger)
	}
}

// TestSectionEditAnswersSection: renaming a section answers with the
// section; moving it to the side area changes the page's layout, so htmx
// is asked to reload the page.
func TestSectionEditAnswersSection(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path

	edit := func(area string) *http.Response {
		t.Helper()
		page := mustGet(t, srv, client, boardURL+"?edit")
		section := regexp.MustCompile(`id="dsec-(\d+)"`).FindSubmatch(page)[1]
		version := regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(page)[1]
		form := url.Values{"csrf": {csrf}, "board_id": {boardIDFrom(boardURL)}, "version": {string(version)}, "title": {"Renamed"},
			"size": {"medium"}, "sort": {"manual"}, "area": {area}, "span": {"0"}, "rows": {"1"}, "mobile": {""},
			"was_area": {"main"}, "was_span": {"0"}}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sections/"+string(section)+"/edit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "dsec-"+string(section))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp = edit("main")
	body := readAll(t, resp)
	resp.Body.Close()
	if !strings.HasPrefix(strings.TrimSpace(body), "<section") || !strings.Contains(body, "Renamed") {
		t.Fatalf("rename: expected the section alone, got %d:\n%.300s", resp.StatusCode, body)
	}

	resp = edit("side")
	resp.Body.Close()
	if resp.Header.Get("HX-Refresh") != "true" {
		t.Fatalf("area change: expected a page reload, got %d %v", resp.StatusCode, resp.Header)
	}
}

// TestPagesCompressed: HTML goes out gzipped to browsers that accept it;
// the CSRF token in it changes with every answer (BREACH) and still
// passes the check.
func TestPagesCompressed(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	boardURL := resp.Request.URL.Path

	token := func() string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+boardURL, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := http.DefaultTransport.RoundTrip(withCookies(client, req))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.Header.Get("Content-Encoding") != "gzip" || resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("page not compressed: %v", resp.Header)
		}
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		page, _ := io.ReadAll(zr)
		m := csrfRe.FindSubmatch(page)
		if m == nil {
			t.Fatalf("no token in page:\n%.300s", page)
		}
		return string(m[1])
	}
	a, b := token(), token()
	if a == b {
		t.Fatal("CSRF token repeats across answers")
	}
	res := postForm(t, client, srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections", url.Values{"csrf": {a}, "title": {"S"},
		"version": {string(regexp.MustCompile(`data-version="(\d+)"`).FindSubmatch(mustGet(t, srv, client, boardURL))[1])}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("masked token rejected: %d", res.StatusCode)
	}
}

// withCookies adds the client's cookies to req, for a raw RoundTrip.
func withCookies(client *http.Client, req *http.Request) *http.Request {
	for _, c := range client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	return req
}
