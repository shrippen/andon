package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestNews: every site in its order; a pinned Reddit post is left out,
// YouTube counts views; a site that fails is named, the rest stays.
func TestNews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			if r.URL.Query().Get("tags") != "front_page" {
				t.Errorf("hn query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"hits":[{"objectID":"7","title":"Show HN: X","url":"https://x.test","points":12,"num_comments":3,"created_at_i":1759820000},` +
				`{"objectID":"8","title":"Ask HN: Y","points":5,"num_comments":9,"created_at_i":1759810000}]}`))
		case "/hottest.json":
			w.Write([]byte(`[{"title":"L","url":"","score":4,"comment_count":1,"created_at":"2026-10-07T10:00:00Z","comments_url":"https://lobste.rs/s/a"}]`))
		case "/r/selfhosted/hot.json":
			w.Write([]byte(`{"data":{"children":[{"data":{"title":"Pinned","stickied":true}},` +
				`{"data":{"title":"R","url":"https://r.test","score":90,"num_comments":20,"created_utc":1759820000.0,"permalink":"/r/selfhosted/comments/1"}}]}}`))
		case "/feeds/videos.xml":
			w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/"><title>Club</title>` +
				`<entry><title>V</title><link rel="alternate" href="https://www.youtube.com/watch?v=1"/><published>2026-10-06T10:00:00+00:00</published>` +
				`<media:group><media:community><media:starRating count="40"/><media:statistics views="1200"/></media:community></media:group></entry></feed>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	defer sources.SetReadingBases(srv.URL)()

	raw, err := sources.NewsData.Fetch(context.Background(), sources.Ctx{URL: "https://news.ycombinator.com",
		Options: map[string]any{"sites": "hackernews, lobsters, r/selfhosted, youtube:UC1, r/gone"}})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.NewsDataset)
	if len(data.Items) != 5 || len(data.Failed) != 1 || data.Failed[0] != "r/gone" {
		t.Fatalf("items %+v failed %v", data.Items, data.Failed)
	}
	hn, ask, lob, red, yt := data.Items[0], data.Items[1], data.Items[2], data.Items[3], data.Items[4]
	if hn.URL != "https://x.test" || hn.Link != "https://news.ycombinator.com/item?id=7" || hn.Points != 12 || hn.At.IsZero() {
		t.Fatalf("hn %+v", hn)
	}
	if ask.URL != ask.Link {
		t.Fatalf("a post without a link points to its discussion: %+v", ask)
	}
	if lob.URL != "https://lobste.rs/s/a" || red.Feed != "selfhosted" || red.Link != srv.URL+"/r/selfhosted/comments/1" || red.Points != 90 {
		t.Fatalf("lobsters %+v reddit %+v", lob, red)
	}
	if yt.Feed != "Club" || yt.Views != 1200 || yt.Points != 40 || yt.URL != "https://www.youtube.com/watch?v=1" {
		t.Fatalf("youtube %+v", yt)
	}
}

// TestNewsNoneAnswered: an error when no site answers; the default
// sites when none are set.
func TestNewsNoneAnswered(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	defer sources.SetReadingBases(srv.URL)()
	if _, err := sources.NewsData.Fetch(context.Background(), sources.Ctx{URL: "https://news.ycombinator.com"}); err == nil {
		t.Fatal("no error")
	}
}

// TestTwitch: one app token for both runs, logins as user_login.
func TestTwitch(t *testing.T) {
	tokens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			r.ParseForm()
			if r.Method != http.MethodPost || r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "sec" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			tokens++
			w.Write([]byte(`{"access_token":"app","expires_in":5000000}`))
		case "/helix/streams":
			if r.Header.Get("Client-Id") != "cid" || r.Header.Get("Authorization") != "Bearer app" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.URL.Query()["user_login"]; len(got) != 2 || got[0] != "one" {
				t.Errorf("logins %v", got)
			}
			w.Write([]byte(`{"data":[{"user_login":"one","title":"T","game_name":"Art","viewer_count":7,"started_at":"2026-10-07T09:00:00Z"}]}`))
		}
	}))
	defer srv.Close()
	defer sources.SetReadingBases(srv.URL)()

	sctx := sources.Ctx{URL: srv.URL, Secret: "cid:sec", Options: map[string]any{"channels": "One, two"}}
	for range 2 {
		raw, err := sources.TwitchData.Fetch(context.Background(), sctx)
		if err != nil {
			t.Fatal(err)
		}
		data := raw.(*sources.TwitchDataset)
		if data.Channels != 2 || len(data.Live) != 1 || data.Live[0].Viewers != 7 || data.Live[0].URL != "https://www.twitch.tv/one" {
			t.Fatalf("data %+v", data)
		}
	}
	if tokens != 1 {
		t.Fatalf("tokens %d", tokens)
	}
	if _, err := sources.TwitchData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "cid:sec"}); err == nil {
		t.Fatal("no channels, no error")
	}
	// A changed secret asks for a token of its own, here refused.
	if _, err := sources.TwitchData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "cid:wrong", Options: sctx.Options}); err == nil {
		t.Fatal("changed secret reused the cached token")
	}
}

// TestDemoReading: the world's posts and live channel decode.
func TestDemoReading(t *testing.T) {
	now := time.Now().UTC()
	news, tw := sources.DemoNews(now), sources.DemoTwitch(now)
	if len(news.Items) != 5 || news.Items[0].At.IsZero() || news.Items[4].Title == "" || len(tw.Live) != 1 || tw.Live[0].Since.IsZero() {
		t.Fatalf("news %+v twitch %+v", news, tw)
	}
}
