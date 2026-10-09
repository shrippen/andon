package sources_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// fake answers fixed JSON per path and checks one auth header.
func fake(t *testing.T, header, value string, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != "" && r.Header.Get(header) != value {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHeadscaleDevices(t *testing.T) {
	srv := fake(t, "Authorization", "Bearer key", map[string]string{
		"GET /api/v1/node": `{"nodes": [{"givenName": "nas", "online": true, "expiry": "0001-01-01T00:00:00Z", "forcedTags": ["tag:server"], "validTags": ["tag:server", "tag:home"]},
			{"name": "pi", "online": false, "lastSeen": "2026-09-01T10:00:00Z", "expiry": "2026-10-01T00:00:00Z"}]}`,
	})
	out, err := sources.TailscaleData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "key"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.TailscaleDataset)
	if !d.Headscale || len(d.Devices) != 2 || len(d.Devices[0].Tags) != 2 || !d.Devices[0].KeyExpiry.IsZero() || d.Devices[1].Online || d.Devices[1].KeyExpiry.IsZero() {
		t.Fatalf("devices: %+v", d)
	}
}

func TestOPNsenseGateway(t *testing.T) {
	srv := fake(t, "Authorization", "Basic a2V5OnNlY3JldA==", map[string]string{
		"GET /api/core/firmware/status": `{"status": "update", "product_version": "25.7.2", "upgrade_packages": [{}, {}]}`,
		"GET /api/routes/gateway/status": `{"items": [{"name": "WAN", "status": "none", "delay": "11.4 ms", "loss": "0.0 %"},
			{"name": "LTE", "status": "down", "status_translated": "Offline", "loss": "100.0 %"}]}`,
	})
	out, err := sources.GatewayData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "key:secret"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.GatewayDataset)
	if d.Updates != 2 || d.Version != "25.7.2" || !d.Gateways[0].Up || d.Gateways[0].DelayMS != 11.4 || d.Gateways[1].Up || d.Gateways[1].Loss != 100 {
		t.Fatalf("gateway: %+v", d)
	}
}

func TestSonarr(t *testing.T) {
	srv := fake(t, "X-Api-Key", "k", map[string]string{
		"GET /api/v3/system/status":  `{"appName": "Sonarr", "version": "4.0.15"}`,
		"GET /api/v3/health":         `[{"type": "error", "message": "No download client"}]`,
		"GET /api/v3/queue":          `{"totalRecords": 2, "records": [{"title": "A", "trackedDownloadStatus": "warning"}, {"title": "B", "trackedDownloadStatus": "ok"}]}`,
		"GET /api/v3/wanted/missing": `{"totalRecords": 7}`,
		"GET /api/v3/calendar":       `[{"series": {"title": "Dark"}, "seasonNumber": 2, "episodeNumber": 5, "airDateUtc": "2030-01-01T20:00:00Z"}]`,
	})
	out, err := sources.ArrData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.ArrDataset)
	if d.Queue != 2 || len(d.Stuck) != 1 || d.Missing != 7 || d.Health[0].Level != "error" || d.Upcoming[0].Title != "Dark 2x05" {
		t.Fatalf("arr: %+v", d)
	}
}

// TestRadarrShowsTheComingRelease: Radarr's calendar lists a film when
// any of its releases falls in the window; the tile shows that coming
// release, not the cinema start months ago.
func TestRadarrShowsTheComingRelease(t *testing.T) {
	now := time.Now().UTC()
	cinema, digital := now.AddDate(0, -4, 0).Format(time.RFC3339), now.AddDate(0, 0, 3).Format(time.RFC3339)
	srv := fake(t, "X-Api-Key", "k", map[string]string{
		"GET /api/v3/system/status":  `{"appName": "Radarr", "version": "5.26.2"}`,
		"GET /api/v3/health":         `[]`,
		"GET /api/v3/queue":          `{"totalRecords": 0, "records": []}`,
		"GET /api/v3/wanted/missing": `{"totalRecords": 0}`,
		"GET /api/v3/calendar":       `[{"title": "Colony", "inCinemas": "` + cinema + `", "digitalRelease": "` + digital + `"}]`,
	})
	out, err := sources.ArrData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.ArrDataset)
	if len(d.Upcoming) != 1 || d.Upcoming[0].At.Format(time.RFC3339) != digital {
		t.Fatalf("upcoming: %+v, want %s", d.Upcoming, digital)
	}
}

// TestJellyfin: the token goes in the Authorization header; Jellyfin
// 10.11 and later reject the legacy X-Emby-Token with 401.
func TestJellyfin(t *testing.T) {
	srv := fake(t, "Authorization", `MediaBrowser Token="k"`, map[string]string{
		"GET /System/Info":  `{"Version": "10.10.7", "HasUpdateAvailable": true}`,
		"GET /Items/Counts": `{"MovieCount": 12, "SeriesCount": 3, "EpisodeCount": 40}`,
		"GET /Sessions":     `[{"UserName": "anna", "NowPlayingItem": {"Name": "E1", "SeriesName": "Dark"}}, {"UserName": "idle"}]`,
	})
	out, err := sources.MediaServerData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.MediaServerDataset)
	if !d.Update || d.Movies != 12 || len(d.Streams) != 1 || d.Streams[0].Title != "Dark" {
		t.Fatalf("jellyfin: %+v", d)
	}
}

func TestVaultwardenLogin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("token") != "admintoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "VW_ADMIN", Value: "jwt"})
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/users", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("VW_ADMIN"); err != nil || c.Value != "jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`[{"email": "a@x", "twoFactorEnabled": false, "userEnabled": true, "lastActive": "2026-09-20 10:00:00 UTC"}]`))
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`"1.34.3"`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, err := sources.VaultwardenData.Fetch(t.Context(), sources.Ctx{URL: srv.URL, Secret: "admintoken"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.VaultwardenDataset)
	if d.Version != "1.34.3" || len(d.Users) != 1 || d.Users[0].TwoFactor || d.Users[0].LastActive.Day() != 20 {
		t.Fatalf("vaultwarden: %+v", d)
	}
}

func TestGrocyAndTibber(t *testing.T) {
	grocy := fake(t, "GROCY-API-KEY", "k", map[string]string{
		"GET /api/stock/volatile": `{"expired_products": [{"product": {"name": "Joghurt"}, "best_before_date": "2026-09-20"}],
			"missing_products": [{"name": "Kaffee", "amount_missing": 1}]}`,
		"GET /api/chores": `[{"chore_name": "Bad", "next_estimated_execution_time": "2026-09-24 10:00:00"}, {"chore_name": "nie", "next_estimated_execution_time": null}]`,
	})
	out, err := sources.GrocyData.Fetch(t.Context(), sources.Ctx{URL: grocy.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(*sources.GrocyDataset)
	if len(g.Expired) != 1 || g.Expired[0].Name != "Joghurt" || g.Missing[0].Name != "Kaffee" || len(g.Chores) != 1 {
		t.Fatalf("grocy: %+v", g)
	}

	tibber := fake(t, "Authorization", "Bearer t", map[string]string{
		"POST /": `{"data": {"viewer": {"homes": [{"appNickname": "Zuhause",
			"currentSubscription": {"priceInfo": {"current": {"total": 0.31, "energy": 0.12, "level": "NORMAL", "currency": "EUR"},
				"today": [{"total": 0.30, "energy": 0.11, "startsAt": "2026-09-25T00:00:00+02:00"}], "tomorrow": []}},
			"consumption": {"nodes": [{"from": "2026-09-24T00:00:00+02:00", "cost": 2.1, "consumption": 7.5}]}}]}}}`,
	})
	out, err = sources.TibberData.Fetch(t.Context(), sources.Ctx{URL: tibber.URL + "/", Secret: "t"})
	if err != nil {
		t.Fatal(err)
	}
	e := out.(*sources.TibberDataset)
	if e.Current != 0.31 || e.CurrentEnergy != 0.12 || len(e.Prices) != 1 || e.Prices[0].Energy != 0.11 || e.Days[0].KWh != 7.5 || e.Days[0].Day != "2026-09-24" {
		t.Fatalf("tibber: %+v", e)
	}
}

// TestGitHubReviewsAndOwnPRs: with a token the dataset lists PRs waiting
// for my review and my own open PRs, from the search API.
func TestGitHubReviewsAndOwnPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" {
			w.Write([]byte(`[]`))
			return
		}
		item := `{"title":"Fix login","number":7,"html_url":"https://github.com/a/b/pull/7","pull_request":{},
			"updated_at":"2026-09-01T10:00:00Z","repository_url":"https://api.github.com/repos/a/b"}`
		if strings.Contains(r.URL.Query().Get("q"), "review-requested:@me") {
			w.Write([]byte(`{"items":[` + item + `]}`))
			return
		}
		w.Write([]byte(`{"items":[` + item + `,` + item + `]}`))
	}))
	defer srv.Close()

	raw, err := sources.GitHubData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "t"})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.GitHubDataset)
	if len(data.Reviews) != 1 || data.Reviews[0].Repo != "a/b" || data.Reviews[0].Number != 7 || len(data.MyPRs) != 2 {
		t.Fatalf("dataset: %+v", data)
	}
}

// TestGitHubDownloads: a repo with a release adds up the downloads of
// every asset over all release pages.
func TestGitHubDownloads(t *testing.T) {
	page := func(n, count int) string {
		var list []string
		for range n {
			list = append(list, `{"assets":[{"download_count":`+strconv.Itoa(count)+`},{"download_count":1}]}`)
		}
		return "[" + strings.Join(list, ",") + "]"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/a/b":
			w.Write([]byte(`{"default_branch":"main"}`))
		case "/repos/a/b/releases/latest":
			w.Write([]byte(`{"tag_name":"v2"}`))
		case "/repos/a/b/releases":
			if r.URL.Query().Get("page") == "1" {
				w.Write([]byte(page(100, 2)))
				return
			}
			w.Write([]byte(page(1, 5)))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	raw, err := sources.GitHubData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Options: map[string]any{"repos": []any{"a/b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := raw.(*sources.GitHubDataset).Repos[0].Downloads; got != 100*3+6 {
		t.Fatalf("downloads %d", got)
	}
}

// TestDemoGitHubDownloadDays: the demo's day totals count back from the
// current total, yesterday last.
func TestDemoGitHubDownloadDays(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	repo := sources.DemoGitHub(now).Repos[0]
	days := repo.DownloadDays
	if len(days) == 0 || days[len(days)-1].Day != "2026-10-04" || days[len(days)-1].Total >= repo.Downloads || days[0].Total >= days[1].Total {
		t.Fatalf("days %+v of %d", days, repo.Downloads)
	}
}

// TestGitHubOwnerRepos: option owner lists the owner's repos, forks and
// archived ones left out unless asked for, explicit repos kept once.
func TestGitHubOwnerRepos(t *testing.T) {
	list := `[{"full_name":"me/app","owner":{"login":"me"}},{"full_name":"me/fork","fork":true,"owner":{"login":"me"}},
		{"full_name":"me/old","archived":true,"owner":{"login":"me"}},{"full_name":"org/lib","owner":{"login":"org"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/repos", "/user/repos":
			w.Write([]byte(list))
		case "/repos/me/app", "/repos/me/fork", "/repos/me/old", "/repos/x/y":
			w.Write([]byte(`{"default_branch":"main"}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	names := func(opts map[string]any, secret string) []string {
		raw, err := sources.GitHubData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: secret, Options: opts})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range raw.(*sources.GitHubDataset).Repos {
			out = append(out, r.Name)
		}
		return out
	}
	if got := strings.Join(names(map[string]any{"owner": "me", "repos": []any{"x/y", "Me/App"}}, ""), " "); got != "x/y Me/App" {
		t.Fatalf("public: %s", got)
	}
	if got := strings.Join(names(map[string]any{"owner": "me", "forks": true, "archived": true}, "t"), " "); got != "me/app me/fork me/old" {
		t.Fatalf("with token, all: %s", got)
	}
}

// TestGitHubDownloadsMemo: within the interval the releases are not read
// again; after it they are.
func TestGitHubDownloadsMemo(t *testing.T) {
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	defer sources.SetClock(func() time.Time { return at })()
	reads, count := 0, 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/memo/b":
			w.Write([]byte(`{"default_branch":"main"}`))
		case "/repos/memo/b/releases/latest":
			w.Write([]byte(`{"tag_name":"v1"}`))
		case "/repos/memo/b/releases":
			reads++
			w.Write([]byte(`[{"assets":[{"download_count":` + strconv.Itoa(count) + `}]}]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	fetch := func() *sources.GitHubDataset {
		raw, err := sources.GitHubData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Options: map[string]any{"repos": []any{"memo/b"}}})
		if err != nil {
			t.Fatal(err)
		}
		return raw.(*sources.GitHubDataset)
	}
	first := fetch()
	count, at = 20, at.Add(30*time.Minute)
	second := fetch()
	at = at.Add(31 * time.Minute)
	third := fetch()
	if reads != 2 || first.Repos[0].Downloads != 10 || second.Repos[0].Downloads != 10 || third.Repos[0].Downloads != 20 || !first.DownloadsAuto {
		t.Fatalf("reads %d: %d %d %d", reads, first.Repos[0].Downloads, second.Repos[0].Downloads, third.Repos[0].Downloads)
	}
}

// TestDownloadsEvery: automatic is hourly; a number of minutes overrides
// it, at least the dataset's own interval.
func TestDownloadsEvery(t *testing.T) {
	cases := []struct {
		opts map[string]any
		want time.Duration
		auto bool
	}{
		{nil, time.Hour, true},
		{map[string]any{"downloads_minutes": "auto"}, time.Hour, true},
		{map[string]any{"downloads_minutes": 30.0}, 30 * time.Minute, false},
		{map[string]any{"downloads_minutes": 1.0}, 5 * time.Minute, false},
	}
	for _, c := range cases {
		got, auto := sources.DownloadsEvery(sources.Ctx{Options: c.opts})
		if got != c.want || auto != c.auto {
			t.Errorf("%v: %v %v", c.opts, got, auto)
		}
	}
}

// TestKDEStore: option user lists all of a user's entries over pages,
// option ids adds others once; an empty count is unknown (0).
func TestKDEStore(t *testing.T) {
	entry := func(id, downloads string) string {
		return `{"id":` + id + `,"name":"E` + id + `","version":"1.0","typename":"Plasma 6 Applets","downloads":` + downloads +
			`,"changed":"2026-09-20T14:54:06+00:00","detailpage":"https://store.kde.org/p/` + id + `"}`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/ocs/v1/content/data" && q.Get("user") == "me" && q.Get("page") == "0":
			w.Write([]byte(`{"status":"ok","totalitems":3,"itemsperpage":2,"data":[` + entry("1", "39") + `,` + entry("2", `""`) + `]}`))
		case r.URL.Path == "/ocs/v1/content/data" && q.Get("user") == "me" && q.Get("page") == "1":
			w.Write([]byte(`{"status":"ok","totalitems":3,"itemsperpage":2,"data":[` + entry("3", "7") + `]}`))
		case r.URL.Path == "/ocs/v1/content/data/9":
			w.Write([]byte(`{"status":"ok","data":[{"id":9,"name":"E9","downloads":5,"description":` +
				`"<a href=\"https://github.com/Me/E9/issues\">GitHub</a>, <a href=\"https://github.com/me/e9.git\">git</a>"}]}`))
		case r.URL.Path == "/ocs/v1/content/data/1":
			w.Write([]byte(`{"status":"ok","data":[` + entry("1", "39") + `]}`))
		default:
			w.Write([]byte(`{"status":"failed","statuscode":101,"data":[]}`))
		}
	}))
	defer srv.Close()

	raw, err := sources.KDEStoreData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Options: map[string]any{"user": "me", "ids": []any{9.0, "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	items := raw.(*sources.KDEStoreDataset).Downloads()
	if len(items) != 4 || items[0].ID != "9" || items[1].ID != "1" || items[1].Total != 39 || items[2].Total != 0 ||
		items[3].URL != "https://store.kde.org/p/3" || items[1].Released.IsZero() {
		t.Fatalf("items %+v", items)
	}
	if repos := raw.(*sources.KDEStoreDataset).Items[0].Repos; !slices.Equal(repos, []string{"me/e9"}) {
		t.Fatalf("linked repos %v", repos)
	}
}

// TestKDEStoreNeedsEntries: without user or ids there is nothing to read;
// the connection says so instead of showing an empty list.
func TestKDEStoreNeedsEntries(t *testing.T) {
	_, err := sources.KDEStoreData.Fetch(context.Background(), sources.Ctx{URL: "https://api.kde-look.org"})
	if err == nil || err.Error() != "kdestore.no_entries" {
		t.Fatalf("err = %v, want kdestore.no_entries", err)
	}
}

// TestKDEStoreEntryLinks: an entry may be given as its store link; read
// twice (ids and user list), the higher count wins, as the single-entry
// answer lags behind; the store page instead of the API is named.
func TestKDEStoreEntryLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ocs/v1/content/data/7":
			w.Write([]byte(`{"status":"ok","data":[{"id":7,"name":"E7","downloads":30}]}`))
		case "/ocs/v1/content/data":
			w.Write([]byte(`{"status":"ok","totalitems":1,"data":[{"id":7,"name":"E7","downloads":39}]}`))
		default:
			w.Write([]byte(`{"status":"failed","data":[]}`))
		}
	}))
	defer srv.Close()

	raw, err := sources.KDEStoreData.Fetch(context.Background(), sources.Ctx{URL: srv.URL,
		Options: map[string]any{"user": "me", "ids": []any{"https://store.kde.org/p/7"}}})
	if err != nil {
		t.Fatal(err)
	}
	if items := raw.(*sources.KDEStoreDataset).Downloads(); len(items) != 1 || items[0].Total != 39 {
		t.Fatalf("items %+v", items)
	}

	_, err = sources.KDEStoreData.Fetch(context.Background(), sources.Ctx{URL: "https://store.kde.org", Options: map[string]any{"user": "me"}})
	if err == nil || err.Error() != "kdestore.wrong_url" {
		t.Fatalf("err = %v, want kdestore.wrong_url", err)
	}
}

// TestHealthchecks: checks come with status, tags and ping times; the
// key goes in X-Api-Key, never in the URL.
func TestHealthchecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/checks/" || r.Header.Get("X-Api-Key") != "ro-key" || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"checks":[{"name":"borg-nas","tags":"backup nightly","status":"up","last_ping":"2026-10-07T02:01:00+00:00","next_ping":"2026-10-08T02:00:00+00:00","grace":3600,"schedule":"0 2 * * *","n_pings":412},` +
			`{"name":"borg-laptop","tags":"","status":"down","last_ping":null,"grace":3600,"timeout":86400,"n_pings":0}]}`))
	}))
	defer srv.Close()

	raw, err := sources.HealthchecksData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "ro-key"})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.HealthchecksDataset)
	if len(data.Checks) != 2 || data.Checks[0].Tags[1] != "nightly" || data.Checks[0].LastPing.IsZero() || !data.Checks[1].LastPing.IsZero() {
		t.Fatalf("checks %+v", data.Checks)
	}
	if down := data.Down(); len(down) != 1 || down[0].Name != "borg-laptop" || down[0].Timeout != 86400 {
		t.Fatalf("down %+v", down)
	}
}

// TestDemoHealthchecks: the demo reads Studio Weber's checks.
func TestDemoHealthchecks(t *testing.T) {
	data := sources.DemoHealthchecks(time.Now())
	if len(data.Checks) < 4 || len(data.Down()) == 0 || data.Checks[0].LastPing.IsZero() || len(data.Checks[0].Tags) == 0 {
		t.Fatalf("demo %+v", data.Checks)
	}
}

// TestPrometheus: alerts with labels and summary; a query's value, its
// hourly line with a missing hour as nil; "user:pass" logs in as basic.
func TestPrometheus(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "me" || pass != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/alerts":
			w.Write([]byte(`{"status":"success","data":{"alerts":[{"labels":{"alertname":"HostDown","severity":"Critical","instance":"nas:9100"},` +
				`"annotations":{"description":"nas is down"},"state":"firing","activeAt":"2026-10-07T08:00:00Z"},` +
				`{"labels":{"alertname":"Slow"},"annotations":{"summary":"slow"},"state":"pending"}]}}`))
		case "/api/v1/query":
			w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1759824000,"3.5"]},{"metric":{},"value":[1759824000,"1"]}]}}`))
		case "/api/v1/query_range":
			older := strconv.FormatInt(end.Add(-2*time.Hour).Unix(), 10)
			now := strconv.FormatInt(end.Unix(), 10)
			w.Write([]byte(`{"status":"success","data":{"result":[{"values":[[` + older + `,"1"],[` + now + `,"2.5"]]}]}}`))
		}
	}))
	defer srv.Close()
	sctx := sources.Ctx{URL: srv.URL, Secret: "me:pw"}

	raw, err := sources.PrometheusData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	alerts := raw.(*sources.PrometheusDataset).Alerts
	if len(alerts) != 2 || alerts[0].Severity != "critical" || alerts[0].Summary != "nas is down" || !alerts[0].Firing() || alerts[1].Firing() {
		t.Fatalf("alerts %+v", alerts)
	}

	sctx.Params = map[string]any{"query": "node_load1", "hours": 3.0}
	raw, err = sources.PrometheusQuery.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	v := raw.(*sources.PromValue)
	if !v.Found || v.Value != 3.5 || v.Series != 2 || len(v.Hourly) != 3 || *v.Hourly[0] != 1 || v.Hourly[1] != nil || *v.Hourly[2] != 2.5 {
		t.Fatalf("value %+v", v)
	}

	sctx.Params = map[string]any{}
	if _, err := sources.PrometheusQuery.Fetch(context.Background(), sctx); err == nil || err.Error() != "prometheus.no_query" {
		t.Fatalf("empty query: %v", err)
	}
}

// TestDemoPrometheus: the demo has firing alerts and answers any query.
func TestDemoPrometheus(t *testing.T) {
	now := time.Now()
	if a := sources.DemoPrometheus(now).Alerts; len(a) < 2 || !a[0].Firing() || a[0].Instance == "" {
		t.Fatalf("alerts %+v", a)
	}
	if v := sources.DemoPromQuery(now, "up", 6); !v.Found || len(v.Hourly) != 6 {
		t.Fatalf("query %+v", v)
	}
}

// TestDemoPromQueryByName: a query the world names gets its own values.
func TestDemoPromQueryByName(t *testing.T) {
	if v := sources.DemoPromQuery(time.Now(), "node_load1", 24); v.Value != 3.4 || len(v.Hourly) != 24 {
		t.Fatalf("node_load1 %+v", v)
	}
}

// TestNVD: high and critical CVEs, their score and summary, and the
// applications they affect with their version range; operating systems
// and non-vulnerable entries are left out; the key goes in a header.
func TestNVD(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apiKey") != "k" || r.URL.Query().Get("pubStartDate") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		level := r.URL.Query().Get("cvssV3Severity")
		asked = append(asked, level)
		if level != "HIGH" {
			w.Write([]byte(`{"totalResults":0,"vulnerabilities":[]}`))
			return
		}
		w.Write([]byte(`{"totalResults":1,"vulnerabilities":[{"cve":{"id":"CVE-2026-1","published":"2026-10-01T10:00:00.000",` +
			`"descriptions":[{"lang":"es","value":"x"},{"lang":"en","value":"Gitea allows things."}],` +
			`"metrics":{"cvssMetricV31":[{"cvssData":{"baseScore":8.1}}]},` +
			`"configurations":[{"nodes":[{"cpeMatch":[` +
			`{"vulnerable":true,"criteria":"cpe:2.3:a:gitea:gitea:*:*:*:*:*:*:*:*","versionStartIncluding":"1.20.0","versionEndExcluding":"1.24.3"},` +
			`{"vulnerable":true,"criteria":"cpe:2.3:o:linux:linux_kernel:*:*:*:*:*:*:*:*"},` +
			`{"vulnerable":false,"criteria":"cpe:2.3:a:golang:go:*:*:*:*:*:*:*:*"}]}]}]}}]}`))
	}))
	defer srv.Close()

	raw, err := sources.NVDData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "k", Options: map[string]any{"days": 500.0}})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.NVDDataset)
	if data.Days != 120 || len(asked) != 2 || len(data.CVEs) != 1 {
		t.Fatalf("days %d asked %v cves %+v", data.Days, asked, data.CVEs)
	}
	c := data.CVEs[0]
	want := sources.CVEProduct{Vendor: "gitea", Product: "gitea", From: "1.20.0", To: "1.24.3"}
	if c.Score != 8.1 || c.Summary != "Gitea allows things." || len(c.Products) != 1 || c.Products[0] != want || c.Published.IsZero() {
		t.Fatalf("cve %+v", c)
	}
}

// TestDrone: active repos only; builds of other branches are left out
// (tags stay), pull requests count for neither the state nor the line.
func TestDrone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/user/repos":
			w.Write([]byte(`[{"slug":"me/app","default_branch":"main","active":true},{"slug":"me/old","active":false}]`))
		case "/api/repos/me/app/builds":
			w.Write([]byte(`[{"number":5,"status":"success","event":"pull_request","target":"main","started":1759820000,"finished":1759820060},` +
				`{"number":4,"status":"failure","event":"push","target":"main","after":"f00dcafe","started":1759810000,"finished":1759810090},` +
				`{"number":3,"status":"success","event":"push","target":"dev"},` +
				`{"number":2,"status":"success","event":"tag","target":"v1.0.0"}]`))
		}
	}))
	defer srv.Close()

	raw, err := sources.DroneData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	repos := raw.(sources.CISource).CIRepos()
	if len(repos) != 1 || repos[0].Repo != "me/app" || repos[0].Status != sources.CIFailed || len(repos[0].Runs) != 2 ||
		repos[0].Runs[0].Seconds != 90 || repos[0].Runs[0].Commit != "f00dcafe" || repos[0].Runs[1].Event != "tag" {
		t.Fatalf("repos %+v", repos)
	}
}

// TestCIRepos: GitHub names each repo's latest run, Gitea only failed ones.
// A GitHub run with a start time is also a run of the history, with its
// commit, so deploys can be matched with it.
func TestCIRepos(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	runs := (&sources.GitHubDataset{Repos: []sources.GitRepo{{Name: "a/b", CI: "failure", CIAt: at, CICommit: "beef"}}}).CIRepos()
	if r := runs[0].Runs; len(r) != 1 || r[0].Status != sources.CIFailed || !r[0].Started.Equal(at) || r[0].Commit != "beef" {
		t.Fatalf("github run %+v", runs)
	}
	gh := (&sources.GitHubDataset{Repos: []sources.GitRepo{{Name: "a/b", CI: "failure"}, {Name: "a/c", CI: "success"}, {Name: "a/d"}}}).CIRepos()
	gt := (&sources.GiteaDataset{Repos: []sources.Repo{{Name: "a/e", FailedWorkflow: "test"}, {Name: "a/f"}}}).CIRepos()
	if len(gh) != 2 || gh[0].Status != sources.CIFailed || gh[1].Status != sources.CIOK || len(gt) != 1 || gt[0].Step != "test" {
		t.Fatalf("github %+v gitea %+v", gh, gt)
	}
	if d := sources.DemoDrone(time.Now()).CIRepos(); len(d) != 3 || d[1].Status != sources.CIFailed {
		t.Fatalf("demo %+v", d)
	}
}

// TestPBS: stores with fill, groups with their newest backup (named by
// comment), verify runs with their store; the token goes as PBSAPIToken.
func TestPBS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PBSAPIToken=andon@pbs!ro:s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api2/json/status/datastore-usage":
			w.Write([]byte(`{"data":[{"store":"tank","total":100,"used":95}]}`))
		case "/api2/json/admin/datastore/tank/groups":
			w.Write([]byte(`{"data":[{"backup-type":"vm","backup-id":"101","last-backup":1759800000,"backup-count":3,"comment":"ha"},{"backup-type":"ct","backup-id":"105","last-backup":1759000000}]}`))
		case "/api2/json/nodes/localhost/tasks":
			w.Write([]byte(`{"data":[{"worker_id":"tank:v-1","status":"error: missing chunk","starttime":1759810000}]}`))
		}
	}))
	defer srv.Close()
	// Option pools names the TrueNAS pool a store lives on.
	raw, err := sources.PBSData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "andon@pbs!ro:s3cret",
		Options: map[string]any{"pools": map[string]any{"tank": "Tank "}}})
	if err != nil {
		t.Fatal(err)
	}
	d := raw.(*sources.PBSDataset)
	jobs := d.BackupJobs()
	if len(d.Stores) != 1 || d.Stores[0].Used != 95 || d.Stores[0].Pool != "Tank" || len(jobs) != 2 || jobs[0].Item != "ha" || jobs[1].Item != "ct/105" ||
		len(d.Verifies) != 1 || d.Verifies[0].Store != "tank" || d.Verifies[0].OK() {
		t.Fatalf("pbs %+v", d)
	}
}

// TestKopiaBackrest: Kopia's last snapshot with errors fails; Backrest's
// newest run decides, its newest success is the last backup.
func TestKopiaBackrest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, _, ok := r.BasicAuth(); !ok || user != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/sources":
			w.Write([]byte(`{"sources":[{"source":{"host":"pc","userName":"me","path":"/home"},"lastSnapshot":{"startTime":"2026-10-07T01:00:00Z","endTime":"2026-10-07T01:10:00Z","stats":{"errorCount":0},"rootEntry":{"summ":{"numFailed":2}}}},` +
				`{"source":{"host":"pc","path":"/etc"},"lastSnapshot":{"endTime":"2026-10-07T02:00:00Z","stats":{}}}]}`))
		case "/v1.Backrest/GetSummaryDashboard":
			w.Write([]byte(`{"planSummaries":[{"id":"db","recentBackups":{"timestampMs":["1759800000000","1759900000000"],"status":["STATUS_SUCCESS","STATUS_ERROR"]}}]}`))
		}
	}))
	defer srv.Close()
	sctx := sources.Ctx{URL: srv.URL, Secret: "admin:pw"}
	raw, err := sources.KopiaData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	k := raw.(sources.BackupSource).BackupJobs()
	if len(k) != 2 || k[0].Item != "pc:/home" || !k[0].Failed || k[1].Failed || k[0].Last.IsZero() {
		t.Fatalf("kopia %+v", k)
	}
	raw, err = sources.BackrestData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	b := raw.(sources.BackupSource).BackupJobs()
	if len(b) != 1 || !b[0].Failed || b[0].Last.UnixMilli() != 1759800000000 {
		t.Fatalf("backrest %+v", b)
	}
}

// TestDemoBackupTools: every tool reads its demo data, each with a problem.
func TestDemoBackupTools(t *testing.T) {
	now := time.Now()
	for _, tool := range []sources.BackupSource{sources.DemoPBS(now), sources.DemoKopia(now), sources.DemoDuplicati(now), sources.DemoBackrest(now), sources.DemoUrBackup(now)} {
		jobs := tool.BackupJobs()
		problem := false
		for _, j := range jobs {
			problem = problem || j.Failed || now.Sub(j.Last) > 48*time.Hour
			if j.Item == "" || j.Last.IsZero() {
				t.Fatalf("%s: %+v", tool.BackupTool(), j)
			}
		}
		if len(jobs) < 2 || !problem {
			t.Fatalf("%s: %+v", tool.BackupTool(), jobs)
		}
	}
	if d := sources.DemoDuplicati(now); d.Jobs[1].Note == "" {
		t.Fatalf("duplicati note %+v", d.Jobs)
	}
}

// TestPowerSources: PeaNUT's NUT flags, OpenDTU's totals and inverters,
// EVCC's state with and without the old "result" wrapper, and its
// charging sessions of the last two months (cost from the price, else
// energy × price per kWh).
func TestPowerSources(t *testing.T) {
	wrapped := false
	day := func(d int) string { return time.Now().UTC().AddDate(0, 0, -d).Format(time.RFC3339) }
	sessions := `[{"created":"` + day(3) + `","finished":"` + day(3) + `","loadpoint":"Carport","vehicle":"Kombi","chargedEnergy":20.5,"price":6.15},` +
		`{"created":"` + day(9) + `","finished":"` + day(9) + `","loadpoint":"Carport","chargedEnergy":10,"pricePerKWh":0.3},` +
		`{"created":"` + day(90) + `","finished":"` + day(90) + `","loadpoint":"Carport","chargedEnergy":10,"price":3}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions":
			if wrapped {
				w.Write([]byte(`{"result":` + sessions + `}`))
				return
			}
			w.Write([]byte(sessions))
		case "/api/v1/devices":
			w.Write([]byte(`[{"peanut.device_id":"nas","device.model":"Eaton","ups.status":"OB LB","battery.charge":"35","battery.runtime":"240","ups.load":"40"}]`))
		case "/api/livedata/status":
			w.Write([]byte(`{"total":{"Power":{"v":412.3},"YieldDay":{"v":1840},"YieldTotal":{"v":1234.5}},"inverters":[{"name":"Balkon","serial":"1","reachable":true,"producing":true,"AC":{"0":{"Power":{"v":412.3}}}}]}`))
		case "/api/state":
			state := `{"pvPower":412,"homePower":380,"grid":{"power":3650},"battery":{"soc":80},"tariffGrid":0.34,"loadpoints":[{"title":"Carport","vehicleTitle":"Kombi","connected":true,"charging":true,"chargePower":3700,"vehicleSoc":54,"chargedEnergy":6400,"mode":"now"}],"statistics":{"30d":{"chargedKWh":142,"avgPrice":0.27,"solarPercentage":31}}}`
			if wrapped {
				state = `{"result":` + state + `}`
			}
			w.Write([]byte(state))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	sctx := sources.Ctx{URL: srv.URL}

	raw, err := sources.PeaNUTData.Fetch(ctx, sctx)
	if err != nil {
		t.Fatal(err)
	}
	ups := raw.(*sources.UPSDataset).Devices[0]
	if ups.Name != "nas" || !ups.OnBattery || !ups.LowBattery || ups.ReplaceBattery || ups.Runtime != 240 || ups.Charge != 35 {
		t.Fatalf("ups %+v", ups)
	}
	raw, err = sources.OpenDTUData.Fetch(ctx, sctx)
	if err != nil {
		t.Fatal(err)
	}
	if s := raw.(*sources.SolarDataset); s.Power != 412.3 || s.YieldDay != 1840 || len(s.Inverters) != 1 || s.Inverters[0].Power != 412.3 {
		t.Fatalf("solar %+v", s)
	}
	for _, w := range []bool{false, true} {
		wrapped = w
		raw, err = sources.EVCCData.Fetch(ctx, sctx)
		if err != nil {
			t.Fatal(err)
		}
		e := raw.(*sources.EVCCDataset)
		if e.Grid != 3650 || e.BatterySoc != 80 || len(e.Loadpoints) != 1 || e.Loadpoints[0].Charged != 6.4 || e.ChargedKWh30 != 142 {
			t.Fatalf("evcc (wrapped %v) %+v", w, e)
		}
		if len(e.Sessions) != 2 || e.Sessions[0].Price != 6.15 || e.Sessions[0].Vehicle != "Kombi" || e.Sessions[1].Price != 3 || e.Sessions[1].KWh != 10 {
			t.Fatalf("sessions (wrapped %v) %+v", w, e.Sessions)
		}
	}
}

// TestDemoPower: the demo's UPS, panels and wallbox.
func TestDemoPower(t *testing.T) {
	now := time.Now()
	if d := sources.DemoUPS(now, enums.ServiceApcupsd).Devices; len(d) != 1 || d[0].Runtime != 420 || d[0].OnBattery {
		t.Fatalf("apcupsd %+v", d)
	}
	if s := sources.DemoSolar(now); len(s.Inverters) != 2 || s.Inverters[1].Reachable {
		t.Fatalf("solar %+v", s)
	}
	if e := sources.DemoEVCC(now); e.BatterySoc != -1 || !e.Loadpoints[0].Charging || e.SolarPercent30 != 31 {
		t.Fatalf("evcc %+v", e)
	}
}

// TestProxies: Traefik's hosts from router rules with their service's
// state; Caddy's hosts with the first upstream; NPM's hosts with target
// and certificate end after its token login.
func TestProxies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/http/routers":
			w.Write([]byte("[{\"name\":\"immich@docker\",\"rule\":\"Host(`photos.example.org`) || Host(`fotos.example.org`)\",\"service\":\"immich\",\"status\":\"enabled\"}]"))
		case "/api/http/services":
			w.Write([]byte(`[{"name":"immich@docker","serverStatus":{"http://172.18.0.5:2283":"DOWN"}}]`))
		case "/config/apps/http/servers":
			w.Write([]byte(`{"srv0":{"routes":[{"match":[{"host":["wiki.example.org"]}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"bookstack:80"}]}]}]}]}]}}`))
		case "/reverse_proxy/upstreams":
			w.Write([]byte(`[{"address":"bookstack:80","fails":0}]`))
		case "/api/tokens":
			w.Write([]byte(`{"token":"T"}`))
		case "/api/nginx/proxy-hosts":
			if r.Header.Get("Authorization") != "Bearer T" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`[{"domain_names":["kimai.example.org"],"forward_scheme":"http","forward_host":"kimai","forward_port":8001,"enabled":1,"certificate":{"expires_on":"2026-11-01 10:00:00"}}]`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	raw, err := sources.TraefikData.Fetch(ctx, sources.Ctx{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	tr := raw.(*sources.RoutesDataset).Routes
	if len(tr) != 2 || tr[1].Host != "fotos.example.org" || tr[0].Service != "immich" || tr[0].Up || tr[0].Target != "http://172.18.0.5:2283" {
		t.Fatalf("traefik %+v", tr)
	}
	raw, err = sources.CaddyData.Fetch(ctx, sources.Ctx{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if c := raw.(*sources.RoutesDataset).Routes; len(c) != 1 || c[0].Target != "bookstack:80" || c[0].Service != "bookstack" || !c[0].Up {
		t.Fatalf("caddy %+v", c)
	}
	raw, err = sources.NPMData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "me@example.org:pw"})
	if err != nil {
		t.Fatal(err)
	}
	if n := raw.(*sources.RoutesDataset).Routes; len(n) != 1 || n[0].Target != "http://kimai:8001" || !n[0].Up || n[0].CertExpiry.Month() != time.November {
		t.Fatalf("npm %+v", n)
	}
}

// TestTechnitiumFritz: Technitium's day as a DNS filter dataset; the
// FRITZ!Box's PPP connection and DSL rates over TR-064.
func TestTechnitiumFritz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dashboard/stats/get":
			if r.URL.Query().Get("token") != "tok" {
				w.Write([]byte(`{"status":"invalid-token"}`))
				return
			}
			w.Write([]byte(`{"status":"ok","response":{"stats":{"totalQueries":1000,"totalBlocked":100,"totalClients":7},"topBlockedDomains":[{"name":"ads.test","hits":40}]}}`))
		case "/upnp/control/wanpppconn1":
			w.Write([]byte(`<r><NewConnectionStatus>Connected</NewConnectionStatus><NewUptime>2400</NewUptime></r>`))
		case "/upnp/control/wandslifconfig1":
			w.Write([]byte(`<r><NewDownstreamCurrRate>250000</NewDownstreamCurrRate><NewUpstreamCurrRate>40000</NewUpstreamCurrRate></r>`))
		case "/upnp/control/deviceinfo":
			w.Write([]byte(`<r><NewModelName>FRITZ!Box 7590</NewModelName></r>`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	raw, err := sources.TechnitiumData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if d := raw.(*sources.DNSFilterDataset); d.Percent != 10 || d.Clients != 7 || d.TopBlocked[0].Domain != "ads.test" {
		t.Fatalf("technitium %+v", d)
	}
	raw, err = sources.FritzData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "andon:pw"})
	if err != nil {
		t.Fatal(err)
	}
	if f := raw.(*sources.FritzDataset); !f.Connected() || f.Uptime != 2400 || f.DownSync != 250000 || f.Model != "FRITZ!Box 7590" {
		t.Fatalf("fritz %+v", f)
	}
	// Since is when the line came up, to the minute: the same over runs.
	if f := raw.(*sources.FritzDataset); time.Since(f.Since) < 39*time.Minute || time.Since(f.Since) > 42*time.Minute || f.Since.Second() != 0 {
		t.Fatalf("fritz since %v", f.Since)
	}
	now := time.Now()
	if r := sources.DemoRoutes(now, enums.ServiceTraefik).Routes; len(r) != 3 || r[0].Up {
		t.Fatalf("demo traefik %+v", r)
	}
	if d := sources.DemoTechnitium(now); d.Queries == 0 || len(d.TopBlocked) != 2 {
		t.Fatalf("demo technitium %+v", d)
	}
	if f := sources.DemoFritz(now); f.DownSync != 250000 || !f.Connected() || f.Since.After(now) || !f.Since.Equal(sources.DemoFritz(now.Add(time.Minute)).Since) {
		t.Fatalf("demo fritz %+v", f)
	}
	// The demo's earlier connections, some seen down, then today's.
	past := sources.DemoFritzPast(now)
	if len(past) < 3 || past[0].Data.Since.IsZero() || !past[len(past)-1].Data.Since.Equal(sources.DemoFritz(now).Since) {
		t.Fatalf("demo past %+v", past)
	}
}

// TestListening: Tautulli's sessions, Navidrome's Subsonic login and now
// playing, Seerr's counts and approved requests waiting for days.
func TestListening(t *testing.T) {
	old := time.Now().AddDate(0, 0, -5).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v2":
			w.Write([]byte(`{"response":{"data":{"sessions":[{"friendly_name":"theo","full_title":"Ebbe"}],"total_bandwidth":8200}}}`))
		case "/rest/getNowPlaying":
			sum := md5.Sum([]byte("pw" + q.Get("s")))
			if q.Get("u") != "me" || q.Get("t") != hex.EncodeToString(sum[:]) {
				w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
				return
			}
			w.Write([]byte(`{"subsonic-response":{"status":"ok","nowPlaying":{"entry":[{"username":"selin","title":"Nebelhorn","artist":"Theo"}]}}}`))
		case "/rest/getScanStatus":
			w.Write([]byte(`{"subsonic-response":{"status":"ok","scanStatus":{"scanning":false,"count":1834}}}`))
		case "/api/v1/request/count":
			w.Write([]byte(`{"pending":1,"approved":5,"processing":2,"available":38}`))
		case "/api/v1/request":
			w.Write([]byte(`{"results":[{"status":2,"type":"tv","createdAt":"` + old + `","media":{"status":3,"tmdbId":42},"requestedBy":{"displayName":"jonas"}},` +
				`{"status":2,"createdAt":"` + old + `","media":{"status":5}}]}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	raw, err := sources.TautulliData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p := raw.(*sources.PlayDataset); len(p.NowStreams()) != 1 || p.Bandwidth != 8200 {
		t.Fatalf("tautulli %+v", p)
	}
	raw, err = sources.NavidromeData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "me:pw"})
	if err != nil {
		t.Fatal(err)
	}
	if p := raw.(*sources.PlayDataset); len(p.Streams) != 1 || p.Streams[0].Title != "Theo – Nebelhorn" || p.Items != 1834 {
		t.Fatalf("navidrome %+v", p)
	}
	raw, err = sources.SeerrData.Fetch(ctx, sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if s := raw.(*sources.SeerrDataset); s.Approved != 5 || len(s.Stuck) != 1 || s.Stuck[0].Title != "TMDB 42" || s.Stuck[0].By != "jonas" {
		t.Fatalf("seerr %+v", s)
	}

	now := time.Now()
	for _, tool := range []enums.ServiceType{enums.ServiceTautulli, enums.ServiceNavidrome, enums.ServiceAudiobookshelf} {
		if d := sources.DemoPlay(now, tool); len(d.Streams) != 1 {
			t.Fatalf("demo %s %+v", tool, d)
		}
	}
	if d := sources.DemoPlay(now, enums.ServiceJellystat); len(d.Top) != 2 || len(d.Libraries) != 2 {
		t.Fatalf("demo jellystat %+v", d)
	}
	if d := sources.DemoSeerr(now); len(d.Stuck) != 2 || d.Stuck[0].Requested.IsZero() {
		t.Fatalf("demo seerr %+v", d)
	}
}

// TestFirefly: asset accounts, deposits positive with the payer as
// merchant, withdrawals negative, transfers out; the next recurrence date
// and the net worth; all in Sure's shape, marked as Firefly.
func TestFirefly(t *testing.T) {
	today := time.Now().UTC().Format(time.DateOnly)
	next := time.Now().UTC().AddDate(0, 0, 3).Format(time.DateOnly)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts":
			w.Write([]byte(`{"data":[{"id":"1","attributes":{"name":"Giro","current_balance":"1200.50","currency_code":"EUR"}}]}`))
		case "/api/v1/transactions":
			w.Write([]byte(`{"data":[{"id":"7","attributes":{"transactions":[` +
				`{"type":"deposit","date":"` + today + `T10:00:00+02:00","amount":"2380.00","description":"RE-17","source_name":"Northlight","destination_name":"Giro"},` +
				`{"type":"withdrawal","date":"` + today + `T11:00:00+02:00","amount":"59.00","description":"Hosting","source_name":"Giro","destination_name":"Nordhost"},` +
				`{"type":"transfer","date":"` + today + `","amount":"100"}]}}],"meta":{"pagination":{"total_pages":1}}}`))
		case "/api/v1/recurrences":
			w.Write([]byte(`{"data":[{"attributes":{"title":"Hosting","active":true,"type":"withdrawal","transactions":[{"amount":"59.00"}],"repetitions":[{"occurrences":["` + next + `"]}]}}]}`))
		case "/api/v1/summary/basic":
			w.Write([]byte(`{"net-worth-in-EUR":{"monetary_value":"48210.00"}}`))
		}
	}))
	defer srv.Close()
	raw, err := sources.FireflyData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "pat"})
	if err != nil {
		t.Fatal(err)
	}
	d := raw.(*sources.SureDataset)
	if d.From() != enums.ServiceFirefly || len(d.Accounts) != 1 || d.Accounts[0].Balance != 1200.5 || len(d.Transactions) != 2 ||
		d.Transactions[0].Amount != 2380 || d.Transactions[0].Merchant != "Northlight" || d.Transactions[1].Amount != -59 ||
		d.Transactions[0].Date != today || len(d.Recurring) != 1 || d.Recurring[0].Next != next || !d.Recurring[0].Expense || d.NetWorth != 48210 {
		t.Fatalf("firefly %+v", d)
	}
}

// TestGhostfolio: the security token buys a session; value, performance
// and the days come from the performance endpoint. Ghostfolio knows no
// "1m" range (400): a year comes back, Andon keeps its last 30 days and
// their performance, (2000-1000)/20000 = 5 %.
func TestGhostfolio(t *testing.T) {
	day := func(back int) string { return time.Now().UTC().AddDate(0, 0, -back).Format(time.DateOnly) }
	chart := fmt.Sprintf(`[{"date":%q,"value":10000,"netPerformance":0},{"date":%q,"value":20000,"netPerformance":1000},`+
		`{"date":%q,"value":21900,"netPerformance":1900},{"date":%q,"value":22000,"netPerformance":2000}]`, day(100), day(30), day(1), day(0))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/anonymous":
			w.Write([]byte(`{"authToken":"S"}`))
		case "/api/v2/portfolio/performance":
			if r.Header.Get("Authorization") != "Bearer S" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !regexp.MustCompile(`^(1d|1y|5y|max|mtd|wtd|ytd|\d{4})$`).MatchString(r.URL.Query().Get("range")) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"performance":{"currentValueInBaseCurrency":22000,"totalInvestment":17500,"netPerformancePercentage":0.11},"chart":` + chart + `}`))
		case "/api/v1/portfolio/holdings":
			w.Write([]byte(`{"holdings":[{"name":"Bonds","valueInBaseCurrency":6130},{"name":"World","valueInBaseCurrency":14290}]}`))
		}
	}))
	defer srv.Close()
	raw, err := sources.GhostfolioData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "sec"})
	if err != nil {
		t.Fatal(err)
	}
	g := raw.(*sources.GhostfolioDataset)
	if g.Value != 22000 || g.PerformancePct != 5 || len(g.Days) != 3 || g.Holdings[0].Name != "World" {
		t.Fatalf("ghostfolio %+v", g)
	}
	if d := sources.DemoGhostfolio(time.Now()); len(d.Days) != 30 || d.Value == 0 || len(d.Holdings) != 2 {
		t.Fatalf("demo %+v", d)
	}
}
