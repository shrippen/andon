package web_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"testing"
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
