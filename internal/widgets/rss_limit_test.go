package widgets_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/sources"
	"andon/internal/widgets"
)

// The tile's "limit" reaches the feed source: 3 of 10 entries.
func TestRssLimitReachesSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<rss version="2.0"><channel><title>T</title>` +
			strings.Repeat(`<item><title>x</title></item>`, 10) + `</channel></rss>`))
	}))
	defer srv.Close()

	kind, _ := widgets.Get("rss")
	cfg, _ := widgets.Decode("rss", map[string]any{"url": srv.URL, "limit": float64(3)})
	query := kind.Queries(cfg)[0]

	source, _ := sources.Get(query.Source)
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: query.Params})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if n := len(out.(*sources.FeedResult).Items); n != 3 {
		t.Fatalf("got %d items, want 3", n)
	}
}
