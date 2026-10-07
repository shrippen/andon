package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
