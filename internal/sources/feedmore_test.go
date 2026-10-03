package sources_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// TestFeedImageThumb: the tile gets a small copy of a big feed picture;
// the full picture stays for the detail dialog. Pictures and feeds load
// side by side: counted by requests in flight, not by wall time, which
// a slow CI runner (race detector) stretches.
func TestFeedImageThumb(t *testing.T) {
	const pictures, delay = 6, 100 * time.Millisecond
	var inFlight, peak atomic.Int32
	big := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := range 1080 {
		for x := range 1920 {
			big.Set(x, y, color.RGBA{R: uint8(x / 8), G: uint8(y / 5), B: 128, A: 255})
		}
	}
	var pic bytes.Buffer
	if err := jpeg.Encode(&pic, big, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a", "/b":
			time.Sleep(delay)
			var items strings.Builder
			for i := range pictures / 2 {
				fmt.Fprintf(&items, `<item><title>%s%d</title><media:thumbnail url="%s/pic%s%d.jpg"/></item>`, r.URL.Path, i, srv.URL, r.URL.Path[1:], i)
			}
			w.Write([]byte(`<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>A</title>` + items.String() + `</channel></rss>`))
		default:
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(delay)
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(pic.Bytes())
		}
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL + "/a", "urls": []string{srv.URL + "/b"},
		"images": true}})
	if err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p < pictures {
		t.Errorf("%d pictures in flight at most, want %d: they load one after the other", p, pictures)
	}

	items := out.(*sources.FeedResult).Items
	if len(items) != pictures {
		t.Fatalf("%d items", len(items))
	}
	for _, it := range items {
		if it.Image == "" || it.Thumb == "" {
			t.Fatalf("%s: image %d B, thumb %d B", it.Title, len(it.Image), len(it.Thumb))
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(it.Thumb, "data:image/jpeg;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width > 400 || cfg.Height > 200 || len(it.Thumb) > len(it.Image)/4 {
			t.Errorf("thumb %dx%d, %d of %d B", cfg.Width, cfg.Height, len(it.Thumb), len(it.Image))
		}
	}
}

// TestFeedBigPictures: news feeds link 1920 px JPEGs of 150–200 KB. They
// were dropped as too large, so a tile showed pictures only now and then;
// and only the first six items got one. Every shown item gets a thumbnail
// now, and the full picture stays small enough to embed.
func TestFeedBigPictures(t *testing.T) {
	const items = 8
	big := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	rnd := uint32(1)
	for i := range big.Pix {
		rnd = rnd*1664525 + 1013904223
		big.Pix[i] = byte(rnd >> 24)
	}
	var pic bytes.Buffer
	if err := jpeg.Encode(&pic, big, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatal(err)
	}
	if pic.Len() <= 150<<10 {
		t.Fatalf("test picture only %d KB", pic.Len()>>10)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed" {
			var list strings.Builder
			for i := range items {
				fmt.Fprintf(&list, `<item><title>N%d</title><media:thumbnail url="%s/p%d.jpg"/></item>`, i, srv.URL, i)
			}
			w.Write([]byte(`<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>News</title>` + list.String() + `</channel></rss>`))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(pic.Bytes())
	}))
	defer srv.Close()

	source, _ := sources.Get("rss")
	out, err := source.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"url": srv.URL + "/feed", "images": true, "limit": float64(items)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range out.(*sources.FeedResult).Items {
		if it.Thumb == "" || it.Image == "" {
			t.Fatalf("%s: thumb %d B, image %d B", it.Title, len(it.Thumb), len(it.Image))
		}
		if len(it.Image) > len(pic.Bytes()) {
			t.Errorf("%s: embedded picture %d KB, larger than the original", it.Title, len(it.Image)>>10)
		}
	}
}
