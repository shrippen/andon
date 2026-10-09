package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestESPHome: nodes with their ping (unknown stays nil) and the
// dashboard's version from the nodes; Basic auth when a password is set.
func TestESPHome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/devices":
			w.Write([]byte(`{"configured":[{"name":"garagentor","friendly_name":"Garagentor","configuration":"garagentor.yaml","address":"garagentor.local",` +
				`"target_platform":"esp8266","deployed_version":"2026.6.2","current_version":"2026.9.1"},` +
				`{"name":"neu","configuration":"neu.yaml","current_version":"2026.9.1"}],"importable":[]}`))
		case "/ping":
			w.Write([]byte(`{"garagentor.yaml":false,"neu.yaml":null}`))
		}
	}))
	defer srv.Close()

	raw, err := sources.ESPHomeData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "admin:pw"})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.ESPHomeDataset)
	if data.Version != "2026.9.1" || len(data.Devices) != 2 {
		t.Fatalf("data %+v", data)
	}
	g, n := data.Devices[0], data.Devices[1]
	if g.Online == nil || *g.Online || g.Platform != "ESP8266" || g.Deployed != "2026.6.2" || n.Online != nil || n.Friendly != "neu" {
		t.Fatalf("devices %+v %+v", g, n)
	}
}

// TestFediverse: account, version with its software, unread by the
// marker, other notification types folded, own posts with their card.
func TestFediverse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/accounts/verify_credentials":
			w.Write([]byte(`{"id":"42","acct":"studio","display_name":"Studio","url":"https://s.test/@studio","followers_count":412,"following_count":3,"statuses_count":9}`))
		case "/api/v1/instance":
			w.Write([]byte(`{"version":"2.7.2 (compatible; Akkoma 3.15.1)"}`))
		case "/api/v1/markers":
			w.Write([]byte(`{"notifications":{"last_read_id":"99"}}`))
		case "/api/v1/notifications":
			w.Write([]byte(`[{"id":"101","type":"mention","created_at":"2026-10-07T10:00:00Z","account":{"acct":"a@x"},"status":{"content":"<p>Hi <b>there</b></p>","url":"https://s.test/1"}},` +
				`{"id":"99","type":"follow","created_at":"2026-10-06T10:00:00Z","account":{"acct":"b@y"}},` +
				`{"id":"100","type":"admin.report","created_at":"2026-10-06T11:00:00Z","account":{"acct":"c@z"}}]`))
		case "/api/v1/accounts/42/statuses":
			if r.URL.Query().Get("exclude_reblogs") != "true" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"content":"<p>v1 out</p>","url":"https://s.test/2","created_at":"2026-10-05T10:00:00Z","card":{"url":"https://github.com/me/app"}}]`))
		}
	}))
	defer srv.Close()

	raw, err := sources.FediverseData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	d := raw.(*sources.FediverseDataset)
	if d.Account.Followers != 412 || d.Software != "Akkoma" || !d.Marker || len(d.Notes) != 3 {
		t.Fatalf("data %+v", d)
	}
	if !d.Notes[0].Unread || d.Notes[0].Text != "Hi there" || d.Notes[1].Unread || !d.Notes[2].Unread || d.Notes[2].Type != "other" {
		t.Fatalf("notes %+v", d.Notes)
	}
	if len(d.Unread("mention")) != 1 || len(d.Posts) != 1 || d.Posts[0].Link != "https://github.com/me/app" {
		t.Fatalf("posts %+v", d.Posts)
	}
}

// TestFediverseGoToSocial: GoToSocial's version names no software
// ("0.22.1+git-fdff42b"); its v2 instance does in source_url.
func TestFediverseGoToSocial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/verify_credentials":
			w.Write([]byte(`{"id":"1","acct":"andon"}`))
		case "/api/v1/instance":
			w.Write([]byte(`{"version":"0.22.1+git-fdff42b"}`))
		case "/api/v2/instance":
			w.Write([]byte(`{"version":"0.22.1+git-fdff42b","source_url":"https://codeberg.org/superseriousbusiness/gotosocial"}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	raw, err := sources.FediverseData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if d := raw.(*sources.FediverseDataset); d.Software != "GoToSocial" {
		t.Fatalf("software %q", d.Software)
	}
}

// TestDemoESPHomeFediverse: the world's nodes and account decode.
func TestDemoESPHomeFediverse(t *testing.T) {
	now := time.Now().UTC()
	esp, fedi := sources.DemoESPHome(now), sources.DemoFediverse(now)
	if len(esp.Devices) != 4 || esp.Devices[2].Online == nil || *esp.Devices[2].Online {
		t.Fatalf("esphome %+v", esp)
	}
	if fedi.Account.Followers != 412 || len(fedi.Account.FollowerDays) == 0 || len(fedi.Unread("")) != 2 || fedi.Posts[1].Link != "https://store.kde.org/p/2391004" {
		t.Fatalf("fediverse %+v", fedi)
	}
}
