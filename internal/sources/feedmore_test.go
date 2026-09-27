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

// TestFeedsMergeAgeAndImages: two feeds merge newest first, old items
// drop out, and an item's image arrives embedded (the page only loads
// images from itself).
func TestFeedsMergeAgeAndImages(t *testing.T) {
	recent := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC1123Z)
	newer := time.Now().UTC().Add(-time.Hour).Format(time.RFC1123Z)
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC1123Z)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Write([]byte(`<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>A</title>
<item><title>A1</title><pubDate>` + recent + `</pubDate><media:thumbnail url="` + srv.URL + `/pic.png"/></item>
<item><title>A-old</title><pubDate>` + old + `</pubDate></item></channel></rss>`))
		case "/b":
			w.Write([]byte(`<rss version="2.0"><channel><title>B</title>
<item><title>B1</title><pubDate>` + newer + `</pubDate><description>&lt;img src="` + srv.URL + `/pic.png"&gt; text</description></item></channel></rss>`))
		case "/pic.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG"))
		}
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL + "/a", "urls": []string{srv.URL + "/b"},
		"images": true, "max_age": 7.0, "limit": 8.0}})
	if err != nil {
		t.Fatal(err)
	}
	items := out.(*sources.FeedResult).Items
	if len(items) != 2 || items[0].Title != "B1" || items[1].Title != "A1" {
		t.Fatalf("items: %+v", items)
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Image, "data:image/png;base64,") {
			t.Fatalf("image of %s: %q", it.Title, it.Image)
		}
	}
}
