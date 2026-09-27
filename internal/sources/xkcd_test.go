package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/sources"
)

// TestXkcdRandom: a random comic is one of 1…latest, never the missing
// 404, and its own page is linked.
func TestXkcdRandom(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/info.0.json":
			w.Write([]byte(`{"num": 3, "safe_title": "Latest", "img": "` + srv.URL + `/c.png"}`))
		case strings.HasSuffix(r.URL.Path, "/info.0.json"):
			num := strings.Trim(strings.TrimSuffix(r.URL.Path, "/info.0.json"), "/")
			w.Write([]byte(`{"num": ` + num + `, "safe_title": "Comic ` + num + `", "img": "` + srv.URL + `/c.png"}`))
		case r.URL.Path == "/c.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
		}
	}))
	defer srv.Close()
	defer sources.SetBases(srv.URL)()

	for range 10 {
		out, err := sources.XkcdSource{}.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"random": true}})
		if err != nil {
			t.Fatal(err)
		}
		pic := out.(*sources.Picture)
		if !strings.HasPrefix(pic.Title, "Comic ") || !strings.HasSuffix(pic.Link, "/"+strings.TrimPrefix(pic.Title, "Comic ")+"/") {
			t.Fatalf("picture: %+v", pic)
		}
	}
}
