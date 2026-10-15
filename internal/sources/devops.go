package sources

// FreshRSS (feeds), Gitea (code) and Borg Backup Server (backups).

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	giteaPage       = 50
	giteaMaxPages   = 5
	giteaActiveDays = 30
	usecPerSecond   = 1_000_000
	readingList     = "user/-/state/com.google/reading-list"
)

// ── FreshRSS ──

type Feed struct {
	ID, Title, Category string
	URL                 string // the feed's own address
	Unread              int
	Newest              time.Time // zero if unknown
}

type FreshRSSDataset struct {
	URL    string
	Unread int
	Feeds  []Feed
	// Releases are the newest release per GitHub repo among the recent
	// items (feeds like github.com/<repo>/releases.atom), for the update
	// bench.
	Releases []FeedRelease
	// Silent are release feeds whose items link no release at all.
	Silent []Feed
}

// FeedRelease is a release a feed announced: "immich-app/immich",
// "v1.132.3".
type FeedRelease struct {
	Feed, Repo, Version, Link string
	Published                 time.Time
}

var FreshRSSData = source{key: "freshrss.data", ttl: opsTTL, service: enums.ServiceFreshRSS, fetch: fetchFreshRSS}

func fetchFreshRSS(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoFreshRSS(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.FreshRSSApi{URL: sctx.URL, Secret: secret, Verify: sctx.VerifyTLS}
	auth, err := api.Open(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	subs, err := api.Get(ctx, auth, "subscription/list")
	if err != nil {
		return nil, fetchError(err)
	}
	counts, err := api.Get(ctx, auth, "unread-count")
	if err != nil {
		return nil, fetchError(err)
	}
	data := parseFreshRSS(sctx.URL, subs, counts)
	data.Releases, data.Silent = feedReleasesOf(ctx, api, auth, data)
	return data, nil
}

// Release feeds are GitHub's: github.com/<repo>/releases.atom. Their
// items carry the whole notes, so each feed is read on its own, a few
// items, and only again once FreshRSS has a newer item in it.
const (
	releaseItems    = 5
	releaseParallel = 4
)

var releaseFeed = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/(releases|tags)\.atom$`)

// releaseMemo remembers each feed's newest release by its newest item;
// any says the feed links releases at all (pre-releases count here).
type releaseMemo struct {
	newest     time.Time
	release    FeedRelease
	found, any bool
}

var (
	releaseMu   sync.Mutex
	releaseSeen = map[string]releaseMemo{} // FreshRSS URL + feed id
)

// feedReleasesOf reads the release feeds that changed since last time.
func feedReleasesOf(ctx context.Context, api services.FreshRSSApi, auth map[string]string, data *FreshRSSDataset) ([]FeedRelease, []Feed) {
	var feeds []Feed
	for _, f := range data.Feeds {
		if releaseFeed.MatchString(f.URL) {
			feeds = append(feeds, f)
		}
	}
	parallel(ctx, len(feeds), releaseParallel, func(i int) {
		f := feeds[i]
		key := data.URL + "|" + f.ID
		releaseMu.Lock()
		memo, ok := releaseSeen[key]
		releaseMu.Unlock()
		if ok && memo.newest.Equal(f.Newest) {
			return
		}
		items, err := api.Query(ctx, auth, "stream/contents/"+f.ID, url.Values{"n": {strconv.Itoa(releaseItems)}})
		if err != nil {
			return
		}
		got := feedReleases(items)
		memo = releaseMemo{newest: f.Newest, found: len(got) > 0, any: linksReleases(items)}
		if memo.found {
			memo.release = got[0]
		}
		releaseMu.Lock()
		releaseSeen[key] = memo
		releaseMu.Unlock()
	})

	var out []FeedRelease
	var silent []Feed
	releaseMu.Lock()
	for _, f := range feeds {
		m, read := releaseSeen[data.URL+"|"+f.ID]
		switch {
		case m.found:
			out = append(out, m.release)
		case read && !m.any:
			silent = append(silent, f)
		}
	}
	releaseMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Published.After(out[j].Published) })
	return out, silent
}

// releaseLink finds a GitHub release in an item's link:
// https://github.com/immich-app/immich/releases/tag/v1.132.3.
var releaseLink = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/releases/tag/([^/?#]+)`)

// preRelease marks versions no one deploys by default.
var preRelease = regexp.MustCompile(`(?i)(alpha|beta|rc|nightly|dev|preview)`)

// linksReleases says whether any item links a GitHub release.
func linksReleases(items any) bool {
	for _, raw := range asList(asMap(items)["items"]) {
		if alt := asList(asMap(raw)["alternate"]); len(alt) > 0 && releaseLink.MatchString(asStr(asMap(alt[0])["href"])) {
			return true
		}
	}
	return false
}

// feedReleases keeps the newest stable release per repo, newest first.
func feedReleases(items any) []FeedRelease {
	newest := map[string]FeedRelease{}
	for _, raw := range asList(asMap(items)["items"]) {
		it := asMap(raw)
		link := ""
		if alt := asList(it["alternate"]); len(alt) > 0 {
			link = asStr(asMap(alt[0])["href"])
		}
		m := releaseLink.FindStringSubmatch(link)
		if m == nil {
			continue
		}
		version, err := url.PathUnescape(m[2])
		if err != nil || preRelease.MatchString(version) {
			continue
		}
		r := FeedRelease{Feed: asStr(asMap(it["origin"])["streamId"]), Repo: strings.ToLower(m[1]), Version: version, Link: link,
			Published: time.Unix(int64(asFloat(it["published"])), 0).UTC()}
		if old, ok := newest[r.Repo]; !ok || r.Published.After(old.Published) {
			newest[r.Repo] = r
		}
	}
	out := make([]FeedRelease, 0, len(newest))
	for _, r := range newest {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Published.After(out[j].Published) })
	return out
}

func parseFreshRSS(base string, subs, counts any) *FreshRSSDataset {
	data := &FreshRSSDataset{URL: base}
	byID := map[string]*Feed{}
	for _, raw := range asList(asMap(subs)["subscriptions"]) {
		s := asMap(raw)
		f := &Feed{ID: asStr(s["id"]), Title: asStr(s["title"]), URL: asStr(s["url"])}
		if cats := asList(s["categories"]); len(cats) > 0 {
			f.Category = asStr(asMap(cats[0])["label"])
		}
		byID[f.ID] = f
	}
	for _, raw := range asList(asMap(counts)["unreadcounts"]) {
		c := asMap(raw)
		id, n := asStr(c["id"]), int(asFloat(c["count"]))
		if id == readingList {
			data.Unread = n
			continue
		}
		f, ok := byID[id]
		if !ok {
			continue
		}
		f.Unread = n
		if usec, _ := strconv.ParseInt(asStr(c["newestItemTimestampUsec"]), 10, 64); usec > 0 {
			f.Newest = time.Unix(usec/usecPerSecond, 0).UTC()
		}
	}
	for _, f := range byID {
		data.Feeds = append(data.Feeds, *f)
	}
	// Servers without the reading-list total: sum the feeds.
	if data.Unread == 0 {
		for _, f := range data.Feeds {
			data.Unread += f.Unread
		}
	}
	sort.Slice(data.Feeds, func(i, j int) bool { return data.Feeds[i].Title < data.Feeds[j].Title })
	return data
}

// ── Gitea ──

// Issue is an open issue or pull request that concerns the user.
type Issue struct {
	Repo, Title, URL string
	Number           int64
	Pull             bool
	Due, Updated     time.Time // Due zero if none
}

type Repo struct {
	Name, URL      string
	Mirror         bool
	MirrorUpdated  time.Time
	Updated        time.Time
	FailedWorkflow string // latest Actions run failed: its name
}

type GiteaDataset struct {
	URL           string
	User          string
	Notifications int
	Assigned      []Issue
	Reviews       []Issue // PRs waiting for the user's review
	Repos         []Repo
	Stacks        []Stack   // compose stacks, by host and name
	StacksRead    bool      // false: a compose repo could not be read
	Notes         []DocNote // IT docs (options docs_repo, docs_paths), by path
	NotesRead     bool      // false: no vault set, or a folder could not be read
}

var GiteaData = source{key: "gitea.data", ttl: opsTTL, service: enums.ServiceGitea, fetch: fetchGitea}

func fetchGitea(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGitea(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.GiteaApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	data, err := loadGitea(ctx, api, sctx.URL, time.Now())
	if err != nil {
		return nil, fetchError(err)
	}
	data.Notes, data.NotesRead = loadNotes(ctx, api, sctx)
	return data, nil
}

func parseTime(v any) time.Time {
	t, _ := time.Parse(time.RFC3339, asStr(v))
	return t.UTC()
}

func giteaIssues(raw any) []Issue {
	var out []Issue
	for _, item := range asList(raw) {
		m := asMap(item)
		out = append(out, Issue{
			Repo: asStr(asMap(m["repository"])["full_name"]), Title: asStr(m["title"]), URL: asStr(m["html_url"]),
			Number: asInt64(m["number"]), Pull: m["pull_request"] != nil,
			Due: parseTime(m["due_date"]), Updated: parseTime(m["updated_at"]),
		})
	}
	return out
}

func loadGitea(ctx context.Context, api services.GiteaApi, base string, now time.Time) (*GiteaDataset, error) {
	me, err := api.Get(ctx, "user", nil)
	if err != nil {
		return nil, err
	}
	data := &GiteaDataset{URL: base, User: asStr(asMap(me)["login"])}

	if n, err := api.Get(ctx, "notifications/new", nil); err == nil {
		data.Notifications = int(asFloat(asMap(n)["new"]))
	}
	limit := strconv.Itoa(giteaPage)
	assigned, err := api.Get(ctx, "repos/issues/search", url.Values{"type": {"assigned"}, "state": {"open"}, "limit": {limit}})
	if err != nil {
		return nil, err
	}
	data.Assigned = giteaIssues(assigned)
	// review_requested exists since Gitea 1.19; older servers just lack it.
	if reviews, err := api.Get(ctx, "repos/issues/search", url.Values{"review_requested": {"true"}, "type": {"pulls"}, "state": {"open"}, "limit": {limit}}); err == nil {
		data.Reviews = giteaIssues(reviews)
	}

	for page := 1; page <= giteaMaxPages; page++ {
		repos, err := api.Get(ctx, "user/repos", url.Values{"limit": {limit}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		list := asList(repos)
		for _, raw := range list {
			m := asMap(raw)
			if asBool(m["archived"]) {
				continue
			}
			repo := Repo{Name: asStr(m["full_name"]), URL: asStr(m["html_url"]), Mirror: asBool(m["mirror"]),
				MirrorUpdated: parseTime(m["mirror_updated"]), Updated: parseTime(m["updated_at"])}
			if now.Sub(repo.Updated) < giteaActiveDays*24*time.Hour {
				repo.FailedWorkflow = latestFailedRun(ctx, api, repo.Name)
			}
			data.Repos = append(data.Repos, repo)
		}
		if len(list) < giteaPage {
			break
		}
	}

	data.Stacks, data.StacksRead = loadStacks(ctx, api)
	return data, nil
}

// activityWeeks and activityRepos bound the dialog's commit columns.
const (
	activityWeeks = 8
	activityRepos = 8
)

// PushMirror is a repo's push mirror (Gitea → GitHub) and its last sync.
type PushMirror struct {
	Repo, Remote string
	Synced       time.Time
	Error        string
}

// GiteaActivity is what the dialog reads on open: commits per week of
// the recently active repos (oldest week first) and the push mirrors.
type GiteaActivity struct {
	Weeks   map[string][]int
	Mirrors []PushMirror
}

var GiteaActivitySource = source{key: "gitea.activity", ttl: detailTTL, service: enums.ServiceGitea, fetch: fetchGiteaActivity}

func fetchGiteaActivity(ctx context.Context, sctx Ctx) (any, error) {
	now := time.Now().UTC()
	if isDemo(sctx) {
		return DemoGiteaActivity(now), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.GiteaApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	repos, err := api.Get(ctx, "user/repos", url.Values{"limit": {strconv.Itoa(giteaPage)}})
	if err != nil {
		return nil, fetchError(err)
	}
	since := now.AddDate(0, 0, -activityWeeks*daysPerWeek)
	var names []string
	for _, raw := range asList(repos) {
		m := asMap(raw)
		if !asBool(m["archived"]) && !asBool(m["mirror"]) && parseTime(m["updated_at"]).After(since) && len(names) < activityRepos {
			names = append(names, asStr(m["full_name"]))
		}
	}
	out := &GiteaActivity{Weeks: map[string][]int{}}
	var mu sync.Mutex
	parallel(ctx, len(names), activityRepos, func(i int) {
		weeks := make([]int, activityWeeks)
		commits, err := api.Get(ctx, "repos/"+names[i]+"/commits", url.Values{"since": {since.Format(time.RFC3339)}, "limit": {strconv.Itoa(giteaPage)}, "stat": {"false"}})
		if err == nil {
			for _, c := range asList(commits) {
				at := parseTime(asMap(asMap(asMap(c)["commit"])["author"])["date"])
				if w := int(now.Sub(at).Hours() / 24 / daysPerWeek); w >= 0 && w < activityWeeks {
					weeks[activityWeeks-1-w]++
				}
			}
		}
		mirrors, _ := api.Get(ctx, "repos/"+names[i]+"/push_mirrors", nil)
		mu.Lock()
		defer mu.Unlock()
		out.Weeks[names[i]] = weeks
		for _, raw := range asList(mirrors) {
			m := asMap(raw)
			out.Mirrors = append(out.Mirrors, PushMirror{Repo: names[i], Remote: asStr(m["remote_address"]), Synced: parseTime(m["last_update"]), Error: asStr(m["last_error"])})
		}
	})
	sort.Slice(out.Mirrors, func(a, b int) bool { return out.Mirrors[a].Repo < out.Mirrors[b].Repo })
	return out, nil
}

// latestFailedRun returns the latest Actions run's name when it failed.
// Servers without Actions answer 404; that counts as none.
func latestFailedRun(ctx context.Context, api services.GiteaApi, repo string) string {
	runs, err := api.Get(ctx, "repos/"+repo+"/actions/runs", url.Values{"limit": {"1"}})
	if err != nil {
		return ""
	}
	list := asList(asMap(runs)["workflow_runs"])
	if len(list) == 0 {
		return ""
	}
	run := asMap(list[0])
	if asStr(run["conclusion"]) != "failure" && asStr(run["status"]) != "failure" {
		return ""
	}
	name := asStr(run["display_title"])
	if name == "" {
		name = asStr(run["path"])
	}
	return strings.TrimSpace(name)
}

// ── Borg Backup Server ──

type BorgClient struct {
	Name, Status string // online, offline, error, setup
	LastSeen     time.Time
	LastBackup   time.Time // newest successful backup; zero when unknown
	LastFailed   bool      // an enabled plan's latest run failed
	// From /metrics (BBS 2.65+), 0 when unknown: the repositories' size,
	// the newest successful run's archive size and duration.
	RepoBytes, LastBytes float64
	LastSeconds          int
}

// borgJobCompleted is a backup job's result when it succeeded.
const borgJobCompleted = "completed"

type BorgDataset struct {
	URL            string
	Clients        []BorgClient
	Failed24h      int
	Completed24h   int
	Running        int
	UsedBytes      float64
	TotalBytes     float64
	LastBackup     time.Time
	AgentsOutdated int
	ServerUpdate   bool
}

var BorgData = source{key: "borgbackup.data", ttl: opsTTL, service: enums.ServiceBorgBackup, fetch: fetchBorg}

// borgTime reads "2026-09-25 03:00:00" (server local time) or RFC 3339.
func borgTime(v any) time.Time {
	s := asStr(v)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	t, _ := time.ParseInLocation(time.DateTime, s, time.Local)
	return t.UTC()
}

func fetchBorg(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoBorg(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BorgApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	dash, err := api.Get(ctx, "dashboard")
	if err != nil {
		return nil, fetchError(err)
	}
	clients, err := api.Get(ctx, "clients")
	if err != nil {
		return nil, fetchError(err)
	}
	data := parseBorg(sctx.URL, dash, clients)

	// Backup times per client exist only in /summary (per backup plan);
	// older servers lack it, then the times stay unknown.
	if summary, err := api.Get(ctx, "summary"); err == nil {
		addBorgSummary(data, summary)
	}
	if m, err := api.Get(ctx, "metrics"); err == nil {
		addBorgMetrics(data, m)
	}
	return data, nil
}

// addBorgSummary sets each client's newest successful backup over its
// enabled plans, and LastFailed when a plan's latest run failed.
func addBorgSummary(data *BorgDataset, summary any) {
	for _, raw := range asList(asMap(summary)["clients"]) {
		s := asMap(raw)
		c := data.client(asStr(s["name"]))
		if c == nil {
			continue
		}
		for _, plan := range asList(s["backup_plans"]) {
			p := asMap(plan)
			last := asMap(p["last_backup"])
			if !asBool(p["enabled"]) || last == nil {
				continue
			}
			if asStr(last["result"]) != borgJobCompleted {
				c.LastFailed = true
				continue
			}
			if at := borgTime(last["completed_at"]); at.After(c.LastBackup) {
				c.LastBackup = at
			}
		}
	}
}

// addBorgMetrics adds sizes and durations from /metrics: per client its
// repositories' size and its newest successful run over all plans.
func addBorgMetrics(data *BorgDataset, metrics any) {
	m := asMap(metrics)
	names := map[int64]string{}
	newest := map[string]float64{}
	for _, raw := range asList(m["plans"]) {
		p := asMap(raw)
		name := asStr(p["client"])
		names[asInt64(p["client_id"])] = name
		c := data.client(name)
		if c == nil || asFloat(p["last_success_ts"]) <= newest[name] {
			continue
		}
		newest[name] = asFloat(p["last_success_ts"])
		c.LastBytes, c.LastSeconds = asFloat(p["last_success_bytes"]), int(asFloat(p["last_success_duration_seconds"]))
	}
	for _, raw := range asList(m["repositories"]) {
		r := asMap(raw)
		if c := data.client(names[asInt64(r["client_id"])]); c != nil {
			c.RepoBytes += asFloat(r["size_bytes"])
		}
	}
}

// client finds a client by name; nil if the server did not list it.
func (d *BorgDataset) client(name string) *BorgClient {
	for i := range d.Clients {
		if d.Clients[i].Name == name {
			return &d.Clients[i]
		}
	}
	return nil
}

func parseBorg(base string, dash, clients any) *BorgDataset {
	d := asMap(dash)
	jobs, storage := asMap(d["jobs"]), asMap(d["storage"])
	updates := asMap(d["updates"])
	data := &BorgDataset{
		URL: base, Failed24h: int(asFloat(jobs["failed_24h"])), Completed24h: int(asFloat(jobs["completed_24h"])),
		Running: int(asFloat(jobs["running"])), UsedBytes: asFloat(storage["used_bytes"]), TotalBytes: asFloat(storage["total_bytes"]),
		LastBackup: borgTime(asMap(d["archives"])["last_backup_at"]), AgentsOutdated: int(asFloat(updates["agents_outdated"])),
		ServerUpdate: asBool(updates["server_available"]),
	}
	for _, raw := range asList(asMap(clients)["clients"]) {
		c := asMap(raw)
		data.Clients = append(data.Clients, BorgClient{Name: asStr(c["name"]), Status: asStr(c["status"]), LastSeen: borgTime(c["last_heartbeat"]),
			LastBackup: borgTime(c["last_backup_at"])})
	}
	return data
}

func init() {
	Register(FreshRSSData)
	Register(testOf{FreshRSSData, func(d any) map[string]any { return map[string]any{"feeds": len(d.(*FreshRSSDataset).Feeds)} }})
	Register(GiteaData)
	Register(GiteaActivitySource)
	Register(testOf{GiteaData, func(d any) map[string]any { return map[string]any{"user": d.(*GiteaDataset).User} }})
	Register(BorgData)
	Register(testOf{BorgData, func(d any) map[string]any { return map[string]any{"clients": len(d.(*BorgDataset).Clients)} }})
}
