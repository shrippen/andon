package widgets_test

import (
	"testing"

	"andon/internal/widgets"
)

// TestReloadPaces: an image and a joke set their tile's refresh, and
// their query changes once per period so the cache fetches anew.
func TestReloadPaces(t *testing.T) {
	img, _ := widgets.Decode("image", map[string]any{"url": "https://cam.example/x.jpg", "reload": 5.0, "fit": "cover"})
	joke, _ := widgets.Decode("joke", map[string]any{"every": 3.0})
	if img.(widgets.Refresher).RefreshSeconds() != 300 || joke.(widgets.Refresher).RefreshSeconds() != 3*3600 || !img.(widgets.ImageConfig).Cover {
		t.Fatalf("paces: %+v %+v", img, joke)
	}
	kind, _ := widgets.Get("image")
	if q := kind.Queries(img); q[0].Params["fresh"] == 0.0 {
		t.Fatalf("no period in the query: %+v", q)
	}
}

// TestPictureOptions: xkcd asks for a random comic; image only reaches
// the view.
func TestPictureOptions(t *testing.T) {
	cfg, _ := widgets.Decode("xkcd", map[string]any{"random": true, "image_only": true})
	kind, _ := widgets.Get("xkcd")
	if q := kind.Queries(cfg); q[0].Params["random"] != true {
		t.Fatalf("query: %+v", q)
	}
	if v := kind.View(cfg, nil, widgets.ViewCtx{}); v["ImageOnly"] != true {
		t.Fatalf("view: %+v", v)
	}
}

// TestRssOptions: more feeds, pictures and an age limit go to the source.
func TestRssOptions(t *testing.T) {
	cfg, _ := widgets.Decode("rss", map[string]any{"url": "https://a.example/feed", "more_urls": []any{"https://b.example/feed"}, "images": true, "max_age": 14.0})
	kind, _ := widgets.Get("rss")
	p := kind.Queries(cfg)[0].Params
	if urls := p["urls"].([]string); len(urls) != 1 || p["images"] != true || p["max_age"] != 14.0 {
		t.Fatalf("params: %+v", p)
	}
}
