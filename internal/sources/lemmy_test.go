package sources_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestLemmy: one login for two runs; replies and mentions, subscribed
// and own posts; a refused session logs in anew on the next run.
func TestLemmy(t *testing.T) {
	logins, refuse := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/user/login" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["username_or_email"] != "studio" || body["password"] != "pw" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"incorrect_login"}`))
				return
			}
			logins++
			w.Write([]byte(`{"jwt":"s1"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer s1" || refuse {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v3/site":
			w.Write([]byte(`{"version":"0.19.12","my_user":{"local_user_view":{"person":{"name":"studio","actor_id":"https://l.test/u/studio"}}}}`))
		case "/api/v3/user/replies":
			w.Write([]byte(`{"replies":[{"comment":{"content":"Nice","ap_id":"https://l.test/comment/1","published":"2026-10-07T10:00:00.123456Z"},"creator":{"name":"jo"},"post":{"name":"P"},"community":{"name":"kde"}}]}`))
		case "/api/v3/user/mention":
			w.Write([]byte(`{"mentions":[{"comment":{"content":"@studio","ap_id":"https://l.test/comment/2","published":"2026-10-06T10:00:00.5"},"creator":{"name":"al"},"post":{"name":"Q"},"community":{"name":"film"}}]}`))
		case "/api/v3/post/list":
			if r.URL.Query().Get("type_") != "Subscribed" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"posts":[{"post":{"name":"Hot","ap_id":"https://l.test/post/9","published":"2026-10-07T08:00:00Z"},"counts":{"score":96,"comments":18},"community":{"name":"kde"}}]}`))
		case "/api/v3/user":
			w.Write([]byte(`{"person_view":{"counts":{"post_count":14,"comment_count":87}},"posts":[{"post":{"name":"Mine","url":"https://x.test","ap_id":"https://l.test/post/1"},"counts":{"score":5},"community":{"name":"kde"}}]}`))
		}
	}))
	defer srv.Close()

	sctx := sources.Ctx{URL: srv.URL, Secret: "studio:pw"}
	for range 2 {
		raw, err := sources.LemmyData.Fetch(context.Background(), sctx)
		if err != nil {
			t.Fatal(err)
		}
		d := raw.(*sources.LemmyDataset)
		if d.Version != "0.19.12" || d.User.Posts != 14 || len(d.Replies) != 2 || d.Replies[1].Kind != sources.LemmyMention || d.Replies[1].At.IsZero() {
			t.Fatalf("data %+v", d)
		}
		if d.Subscribed[0].URL != "https://l.test/post/9" || d.Subscribed[0].Score != 96 || d.Own[0].URL != "https://x.test" {
			t.Fatalf("posts %+v %+v", d.Subscribed, d.Own)
		}
	}
	if logins != 1 {
		t.Fatalf("logins %d", logins)
	}

	refuse = true
	if _, err := sources.LemmyData.Fetch(context.Background(), sctx); err == nil {
		t.Fatal("refused session, no error")
	}
	refuse = false
	if _, err := sources.LemmyData.Fetch(context.Background(), sctx); err != nil || logins != 2 {
		t.Fatalf("no new login: %v, %d", err, logins)
	}
	if _, err := sources.LemmyData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "studio:bad"}); err == nil {
		t.Fatal("bad password, no error")
	}
}

// TestDemoLemmy: the world's account decodes.
func TestDemoLemmy(t *testing.T) {
	d := sources.DemoLemmy(time.Now().UTC())
	if len(d.Replies) != 1 || d.Replies[0].Kind != sources.LemmyReply || len(d.Subscribed) != 3 || len(d.Own) != 1 || d.Subscribed[0].At.IsZero() {
		t.Fatalf("demo %+v", d)
	}
}
