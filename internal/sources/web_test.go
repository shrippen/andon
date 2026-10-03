package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/sources"
)

func TestHTTPStatusUpWithinDefaultRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	source, err := sources.Get("http_status")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	status := out.(*sources.HTTPStatusResult)
	if !status.Up || status.Code != http.StatusOK {
		t.Fatalf("expected up=true code=200, got %+v", status)
	}
}

// TestHTTPStatusAcceptAddsCodes: accepted codes count on top of 2xx/3xx,
// as in Dashy's statusCheckAcceptCodes: with 401 accepted a login wall is
// up, a plain 200 still is, a 502 is not.
func TestHTTPStatusAcceptAddsCodes(t *testing.T) {
	for code, want := range map[int]bool{http.StatusOK: true, http.StatusUnauthorized: true, http.StatusBadGateway: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		source, _ := sources.Get("http_status")
		out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{
			"url": srv.URL, "accept": []int{http.StatusUnauthorized},
		}})
		srv.Close()
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if status := out.(*sources.HTTPStatusResult); status.Up != want {
			t.Errorf("HTTP %d: up=%v, want %v", code, status.Up, want)
		}
	}
}

func TestHTTPStatusUnreachableIsDataNotError(t *testing.T) {
	source, _ := sources.Get("http_status")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("expected a failed check to be data, not an error: %v", err)
	}
	status := out.(*sources.HTTPStatusResult)
	if status.Up || status.Error == "" {
		t.Fatalf("expected down with an error message, got %+v", status)
	}
}

func TestFeedFetchParsesRSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>Example Feed</title>
<item><title>Hello &amp; World</title><link>https://example.org/1</link>
<pubDate>Mon, 02 Jan 2006 15:04:05 +0000</pubDate><description>&lt;p&gt;Body text&lt;/p&gt;</description></item>
</channel></rss>`))
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL, "limit": float64(8)}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	feed := out.(*sources.FeedResult)
	if feed.Title != "Example Feed" || len(feed.Items) != 1 {
		t.Fatalf("unexpected feed: %+v", feed)
	}
	item := feed.Items[0]
	if item.Title != "Hello & World" || item.Link != "https://example.org/1" || item.Summary != "Body text" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Published == "" {
		t.Fatal("expected a parsed publish date")
	}
}

func TestFeedFetchParsesAtom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom Feed</title>
<entry><title>Entry One</title><link href="https://example.org/a"/>
<updated>2026-01-02T15:04:05Z</updated><summary>Summary text</summary></entry>
</feed>`))
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL, "limit": float64(8)}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	feed := out.(*sources.FeedResult)
	if feed.Title != "Atom Feed" || len(feed.Items) != 1 || feed.Items[0].Link != "https://example.org/a" {
		t.Fatalf("unexpected feed: %+v", feed)
	}
}

func TestFeedFetchRejectsInvalidXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not xml at all"))
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	if _, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL}}); err == nil {
		t.Fatal("expected an error for invalid feed content")
	}
}

func TestFeedFetchRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<rss version="2.0"><channel><title>T</title>
<item><title>1</title></item><item><title>2</title></item><item><title>3</title></item>
</channel></rss>`))
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL, "limit": float64(2)}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(out.(*sources.FeedResult).Items) != 2 {
		t.Fatalf("expected limit=2 to apply, got %d items", len(out.(*sources.FeedResult).Items))
	}
}

func TestPublicIPFetch(t *testing.T) {
	source, err := sources.Get("public_ip")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source.Service() != "" {
		t.Fatalf("expected public_ip to need no connection, got service=%q", source.Service())
	}
}

func TestGlancesFetchUsesBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/4/quicklook":
			w.Write([]byte(`{"cpu": 12.5, "mem": 40.0, "swap": 1.0}`))
		case "/api/4/fs":
			w.Write([]byte(`[{"mnt_point": "/", "percent": 55.0}]`))
		case "/api/4/load":
			w.Write([]byte(`{"min5": 0.8}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	source, err := sources.Get("glances")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	out, err := source.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	stats := out.(*sources.GlancesResult)
	if stats.CPU != 12.5 || stats.Load != 0.8 || len(stats.Disks) != 1 || stats.Disks[0].Percent != 55.0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("expected bearer token header, got %q", gotAuth)
	}
}

func TestHTTPStatusSendsHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api") != "t" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	source, _ := sources.Get("http_status")
	out, _ := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{
		"url": srv.URL, "headers": map[string]string{"X-Api": "t"},
	}})
	if status := out.(*sources.HTTPStatusResult); !status.Up {
		t.Fatalf("header not sent: %+v", status)
	}
}

func TestIconCandidatesForNewSets(t *testing.T) {
	for spec, want := range map[string]string{
		"sh-immich":          "selfhst/icons/svg/immich.svg",
		"mdi-server":         "@mdi/svg@latest/svg/server.svg",
		"fab fa-github":      "svgs/brands/github.svg",
		"fa-regular fa-bell": "svgs/regular/bell.svg",
		"fas fa-rocket":      "svgs/solid/rocket.svg",
	} {
		got := sources.IconCandidates(spec, "")
		if len(got) == 0 || !strings.HasSuffix(got[0], want) {
			t.Errorf("%s: %v", spec, got)
		}
	}
}

// TestHTTPStatusAsksForAPage: a login proxy (Apache mod_auth_openidc)
// answers requests that do not accept HTML with 401 instead of the login
// redirect; the link check asks for a page like a browser does.
func TestHTTPStatusAsksForAPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	source, _ := sources.Get("http_status")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if status := out.(*sources.HTTPStatusResult); !status.Up {
		t.Fatalf("expected up, got %+v", status)
	}
}

// TestHTTPStatusMethodAndTimeout: HEAD when asked, and a slow service
// counts as down after the tile's time limit.
func TestHTTPStatusMethodAndTimeout(t *testing.T) {
	method := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		if r.URL.Path == "/slow" {
			time.Sleep(1500 * time.Millisecond)
		}
	}))
	defer srv.Close()
	source, _ := sources.Get("http_status")

	out, _ := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL, "method": "HEAD"}})
	if !out.(*sources.HTTPStatusResult).Up || method != http.MethodHead {
		t.Fatalf("head: %+v via %s", out, method)
	}
	out, _ = source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL + "/slow", "timeout": 1.0}})
	if res := out.(*sources.HTTPStatusResult); res.Up || res.Error == "" {
		t.Fatalf("timeout: %+v", res)
	}
}

// TestLinkInfoFollowsRedirects: the detail dialog shows each answer on
// the way to the page, a foreign target without its query.
func TestLinkInfoFollowsRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/login?next=%2F", http.StatusFound)
			return
		}
	}))
	defer srv.Close()

	source, _ := sources.Get("link_info")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL + "/"}})
	if err != nil {
		t.Fatal(err)
	}
	info := out.(*sources.LinkInfo)
	want := []sources.LinkHop{{Code: http.StatusFound, Target: "/login"}, {Code: http.StatusOK}}
	if len(info.Hops) != len(want) || info.Hops[0] != want[0] || info.Hops[1] != want[1] || info.Error != "" {
		t.Fatalf("hops %+v, error %q", info.Hops, info.Error)
	}
	if len(info.IPs) != 1 || info.IPs[0] != "127.0.0.1" {
		t.Fatalf("ips %v", info.IPs)
	}
}
