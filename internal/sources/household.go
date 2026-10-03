package sources

// Everyday services:
//
//	vaultwarden  admin token          users, two-factor, version
//	speedtest    Speedtest Tracker    latest down/up/ping against expectations
//	grocy        GROCY-API-KEY        expired, due and missing products, chores
//	dwd          Bright Sky (DWD)     weather warnings for a place
//	github       api.github.com       repos: issues, PRs, CI, latest release
//	tibber       GraphQL              electricity prices and daily cost

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	bitsPerMbit       = 1e6
	githubPerPage     = "100"
	tibberDays        = 30
	dwdTTL            = 15 * time.Minute
	priceTTL          = 15 * time.Minute
	vaultwardenLayout = "2006-01-02 15:04:05 MST"
)

// ── Vaultwarden ──

type VaultUser struct {
	Email              string
	TwoFactor, Enabled bool
	LastActive         time.Time
	Orgs               []string // organizations the account belongs to
}

type VaultwardenDataset struct {
	URL     string
	Version string
	Users   []VaultUser
}

var VaultwardenData = source{key: "vaultwarden.data", ttl: opsTTL, service: enums.ServiceVaultwarden, fetch: fetchVaultwarden}

func fetchVaultwarden(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoVaultwarden(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.VaultwardenApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	users, err := api.Users(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &VaultwardenDataset{URL: sctx.URL, Version: api.Version(ctx)}
	for _, raw := range asList(users) {
		u := asMap(raw)
		user := VaultUser{Email: asStr(u["email"]), TwoFactor: asBool(u["twoFactorEnabled"]),
			Enabled: asBool(u["userEnabled"]), LastActive: vaultTime(u["lastActive"])}
		for _, o := range asList(u["organizations"]) {
			if name := asStr(asMap(o)["name"]); name != "" {
				user.Orgs = append(user.Orgs, name)
			}
		}
		data.Users = append(data.Users, user)
	}
	return data, nil
}

// vaultTime reads "2026-09-25 10:00:00 UTC" or RFC 3339.
func vaultTime(v any) time.Time {
	if t := parseTime(v); !t.IsZero() {
		return t
	}
	t, _ := time.Parse(vaultwardenLayout, asStr(v))
	return t.UTC()
}

// ── Speedtest Tracker ──

type SpeedtestDataset struct {
	URL                  string
	Down, Up, Ping       float64 // Mbit/s, ms
	Jitter               float64 // ms, 0 = unknown
	At                   time.Time
	ExpectDown, ExpectUp float64 // from the options, 0 = none
}

// SpeedResult is one measurement.
type SpeedResult struct {
	At             time.Time
	Down, Up, Ping float64 // Mbit/s, ms
}

// SpeedResults are the measurements of the last weeks, oldest first:
// what the dialogs need for the provider and the time of day.
type SpeedResults struct{ List []SpeedResult }

var SpeedResultsSource = source{key: "speedtest.results", ttl: detailTTL, service: enums.ServiceSpeedtest, fetch: fetchSpeedResults}

// speedResultsMax is how many measurements the dialog reads (four a day
// for a month and some).
const speedResultsMax = 150

func fetchSpeedResults(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoSpeedResults(time.Now().UTC()), nil
	}
	// Only the v1 API with a token lists results.
	if sctx.Secret == "" || asStr(sctx.Options["kind"]) == speedMySpeed {
		return &SpeedResults{}, nil
	}
	api := services.BearerApi(sctx.URL, sctx.Secret, sctx.TLS())
	body, err := api.Get(ctx, "api/v1/results", url.Values{"sort": {"-created_at"}, "page[size]": {strconv.Itoa(speedResultsMax)}})
	if err != nil {
		return nil, fetchError(err)
	}
	return parseSpeedResults(body), nil
}

// parseSpeedResults reads the v1 result list, failed runs left out.
func parseSpeedResults(body any) *SpeedResults {
	out := &SpeedResults{}
	for _, raw := range asList(asMap(body)["data"]) {
		r := asMap(raw)
		if s := asStr(r["status"]); s != "" && s != "completed" {
			continue
		}
		out.List = append(out.List, SpeedResult{At: parseTime(r["created_at"]), Down: asFloat(r["download_bits"]) / bitsPerMbit,
			Up: asFloat(r["upload_bits"]) / bitsPerMbit, Ping: asFloat(r["ping"])})
	}
	sort.Slice(out.List, func(a, b int) bool { return out.List[a].At.Before(out.List[b].At) })
	return out
}

// DemoSpeedResults are four measurements a day for a month; evenings
// are slower.
func DemoSpeedResults(now time.Time) *SpeedResults {
	var p struct {
		Hours                    []int
		Down, Up, Ping           float64
		EveningHour, EveningDrop int
	}
	demoworld.MustDecode("speed", now, &p)
	out := &SpeedResults{}
	for d := 30; d > 0; d-- {
		for _, hour := range p.Hours {
			at := time.Date(now.Year(), now.Month(), now.Day()-d, hour, 0, 0, 0, time.UTC)
			down := p.Down - float64((d*7)%19)
			if hour == p.EveningHour {
				down -= float64(p.EveningDrop) + float64(d%5)*8
			}
			out.List = append(out.List, SpeedResult{At: at, Down: down, Up: p.Up, Ping: p.Ping})
		}
	}
	return out
}

var SpeedtestData = source{key: "speedtest.data", ttl: opsTTL, service: enums.ServiceSpeedtest, fetch: fetchSpeedtest}

func fetchSpeedtest(ctx context.Context, sctx Ctx) (any, error) {
	data := &SpeedtestDataset{URL: sctx.URL, ExpectDown: asFloat(sctx.Options["expect_down"]), ExpectUp: asFloat(sctx.Options["expect_up"])}
	if isDemo(sctx) {
		demoworld.MustDecode("speed.latest", time.Now(), data)
		return data, nil
	}

	if asStr(sctx.Options["kind"]) == speedMySpeed {
		return mySpeed(ctx, sctx, data)
	}

	// v1 API with token; the old open endpoint reports Mbit/s directly.
	api := services.BearerApi(sctx.URL, sctx.Secret, sctx.TLS())
	if sctx.Secret != "" {
		body, err := api.Get(ctx, "api/v1/results/latest", nil)
		if err != nil {
			return nil, fetchError(err)
		}
		r := asMap(asMap(body)["data"])
		data.Down, data.Up = asFloat(r["download_bits"])/bitsPerMbit, asFloat(r["upload_bits"])/bitsPerMbit
		data.Ping, data.At = asFloat(r["ping"]), parseTime(r["created_at"])
		// The raw Ookla result carries the jitter.
		data.Jitter = asFloat(asMap(asMap(r["data"])["ping"])["jitter"])
		return data, nil
	}
	body, err := api.Get(ctx, "api/speedtest/latest", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	r := asMap(asMap(body)["data"])
	data.Down, data.Up, data.Ping, data.At = asFloat(r["download"]), asFloat(r["upload"]), asFloat(r["ping"]), parseTime(r["created_at"])
	return data, nil
}

// ── MySpeed ──
//
// MySpeed (github.com/gnmyt/MySpeed) keeps its tests in /api/speedtests,
// newest first, Mbit/s and ms; its expected speeds are config values.

const (
	speedMySpeed = "myspeed"
	// mySpeedRecent is how many tests are read to find a successful one.
	mySpeedRecent = "5"
)

// mySpeedTimes are the layouts MySpeed stores "created" in (per database).
var mySpeedTimes = []string{time.RFC3339, "2006-01-02 15:04:05.000 -07:00", "2006-01-02 15:04:05"}

// mySpeedHeaders send the password like MySpeed's own UI: URL-encoded in
// x-password, and plain in password where it is Latin-1.
func mySpeedHeaders(password string) map[string]string {
	headers := map[string]string{"Accept": "application/json"}
	if password == "" {
		return headers
	}
	headers["x-password"] = url.QueryEscape(password)
	latin1 := true
	for _, r := range password {
		latin1 = latin1 && r <= 0xFF
	}
	if latin1 {
		headers["password"] = password
	}
	return headers
}

func mySpeed(ctx context.Context, sctx Ctx, data *SpeedtestDataset) (any, error) {
	opts := httpclient.Options{Headers: mySpeedHeaders(sctx.Secret), SkipVerify: !sctx.VerifyTLS}
	opts.Params = url.Values{"limit": {mySpeedRecent}}
	body, _, err := httpclient.GetJSON(ctx, joinPath(sctx.URL, "api/speedtests"), opts)
	if err != nil {
		return nil, newSourceError("%v", err)
	}
	for _, raw := range asList(body) {
		r := asMap(raw)
		if asStr(r["error"]) != "" {
			continue // failed run: keep looking for the last real measurement
		}
		data.Down, data.Up, data.Ping, data.Jitter = asFloat(r["download"]), asFloat(r["upload"]), asFloat(r["ping"]), asFloat(r["jitter"])
		for _, layout := range mySpeedTimes {
			if at, err := time.Parse(layout, asStr(r["created"])); err == nil {
				data.At = at.UTC()
				break
			}
		}
		break
	}

	// Expected speeds: the options win, else MySpeed's own settings.
	if data.ExpectDown == 0 || data.ExpectUp == 0 {
		opts.Params = nil
		if config, _, err := httpclient.GetJSON(ctx, joinPath(sctx.URL, "api/config"), opts); err == nil {
			c := asMap(config)
			if data.ExpectDown == 0 {
				data.ExpectDown = asFloat(c["download"])
			}
			if data.ExpectUp == 0 {
				data.ExpectUp = asFloat(c["upload"])
			}
		}
	}
	return data, nil
}

// ── Grocy ──

// Product is one stock entry; Due is its best-before day.
type Product struct {
	Name    string
	Due     string
	Missing float64 // amount below the minimum stock
}

// Chore is one household chore with its next due time.
type Chore struct {
	Name string
	Due  time.Time
}

type GrocyDataset struct {
	URL                    string
	Expired, Overdue, Soon []Product
	Missing                []Product
	Chores                 []Chore
}

var GrocyData = source{key: "grocy.data", ttl: opsTTL, service: enums.ServiceGrocy, fetch: fetchGrocy}

// Grocy's products due soon: the dataset reaches GrocyAheadDays ahead so
// a tile may look further; "soon" as Grocy counts it is GrocySoonDays.
const (
	GrocySoonDays  = 5
	GrocyAheadDays = 60
)

// SoonWithin lists the products due within days after now (undated ones
// too): SoonWithin(5, now) is Grocy's own "due soon".
func (d *GrocyDataset) SoonWithin(days int, now time.Time) []Product {
	until := now.AddDate(0, 0, days).Format(time.DateOnly)
	var out []Product
	for _, p := range d.Soon {
		if p.Due == "" || p.Due <= until {
			out = append(out, p)
		}
	}
	return out
}

func fetchGrocy(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGrocy(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "GROCY-API-KEY", secret, sctx.TLS())
	stock, err := api.Get(ctx, "api/stock/volatile", url.Values{"due_soon_days": {strconv.Itoa(GrocyAheadDays)}})
	if err != nil {
		return nil, fetchError(err)
	}
	chores, err := api.Get(ctx, "api/chores", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	s := asMap(stock)
	products := func(key string) []Product {
		var out []Product
		for _, raw := range asList(s[key]) {
			m := asMap(raw)
			out = append(out, Product{Name: firstStr(asStr(asMap(m["product"])["name"]), asStr(m["name"])),
				Due: day(m["best_before_date"]), Missing: asFloat(m["amount_missing"])})
		}
		return out
	}
	data := &GrocyDataset{URL: sctx.URL, Expired: products("expired_products"), Overdue: products("overdue_products"),
		Soon: products("due_products"), Missing: products("missing_products")}
	for _, raw := range asList(chores) {
		m := asMap(raw)
		due, _ := time.ParseInLocation(time.DateTime, asStr(m["next_estimated_execution_time"]), time.Local)
		if due.IsZero() {
			continue
		}
		data.Chores = append(data.Chores, Chore{Name: asStr(m["chore_name"]), Due: due.UTC()})
	}
	sort.Slice(data.Chores, func(i, j int) bool { return data.Chores[i].Due.Before(data.Chores[j].Due) })
	return data, nil
}

// ── DWD warnings via Bright Sky ──

// Warning severities as Bright Sky reports them.
const (
	WarnMinor    = "minor"
	WarnModerate = "moderate"
	WarnSevere   = "severe"
	WarnExtreme  = "extreme"
)

type WeatherWarning struct {
	ID            string
	Event         string
	Headline      string
	Severity      string
	Onset, Expire time.Time
	Description   string // what is coming, in full
	Instruction   string // what to do, "" = nothing
}

type DWDDataset struct {
	URL      string
	Place    string
	Warnings []WeatherWarning
}

var DWDData = source{key: "dwd.data", ttl: dwdTTL, service: enums.ServiceDWD, fetch: fetchDWD}

func fetchDWD(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDWD(time.Now().UTC()), nil
	}
	lat, lon := asFloat(sctx.Options["lat"]), asFloat(sctx.Options["lon"])
	if lat == 0 && lon == 0 {
		return nil, newSourceError("options lat/lon missing")
	}
	params := url.Values{"lat": {fmtCoord(lat)}, "lon": {fmtCoord(lon)}}
	body, err := services.BearerApi(sctx.URL, "", sctx.TLS()).Get(ctx, "alerts", params)
	if err != nil {
		return nil, fetchError(err)
	}
	b := asMap(body)
	data := &DWDDataset{URL: sctx.URL, Place: asStr(asMap(b["location"])["name"])}
	for _, raw := range asList(b["alerts"]) {
		a := asMap(raw)
		data.Warnings = append(data.Warnings, WeatherWarning{ID: asStr(a["alert_id"]), Event: firstStr(asStr(a["event_de"]), asStr(a["event_en"])),
			Headline: firstStr(asStr(a["headline_de"]), asStr(a["headline_en"])), Severity: strings.ToLower(asStr(a["severity"])),
			Onset: parseTime(a["onset"]), Expire: parseTime(a["expires"]),
			Description: firstStr(asStr(a["description_de"]), asStr(a["description_en"])), Instruction: firstStr(asStr(a["instruction_de"]), asStr(a["instruction_en"]))})
	}
	return data, nil
}

// fmtCoord renders a coordinate for a query ("52.52").
func fmtCoord(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// ── GitHub ──

type GitRepo struct {
	Name       string
	Issues     int // without pull requests
	PRs        int
	CI         string // conclusion of the latest run on the default branch, "" = none
	CIURL      string
	CIStep     string // a failed run's first failed job and step: "test › go test"
	Release    string
	ReleasedAt time.Time
	PushedAt   time.Time // the last push to any branch
}

type GitHubDataset struct {
	URL           string
	Repos         []GitRepo
	Notifications int
	Reviews       []Issue // open PRs waiting for my review (token only)
	MyPRs         []Issue // my own open PRs (token only)
}

var GitHubData = source{key: "github.data", ttl: opsTTL, service: enums.ServiceGitHub, fetch: fetchGitHub}

func fetchGitHub(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGitHub(time.Now().UTC()), nil
	}
	api := services.BearerApi(sctx.URL, sctx.Secret, sctx.TLS())
	data := &GitHubDataset{URL: sctx.URL}
	for _, raw := range asList(sctx.Options["repos"]) {
		repo, err := loadRepo(ctx, api, asStr(raw))
		if err != nil {
			return nil, fetchError(err)
		}
		data.Repos = append(data.Repos, repo)
	}
	if sctx.Secret != "" {
		if list, err := api.Get(ctx, "notifications", url.Values{"per_page": {githubPerPage}}); err == nil {
			data.Notifications = len(asList(list))
		}
		data.Reviews = githubSearch(ctx, api, "is:open is:pr review-requested:@me")
		data.MyPRs = githubSearch(ctx, api, "is:open is:pr author:@me")
	}
	return data, nil
}

// githubSearch lists open issues or PRs for a search query; best effort,
// a failed search leaves the list empty.
func githubSearch(ctx context.Context, api services.KeyedApi, query string) []Issue {
	raw, err := api.Get(ctx, "search/issues", url.Values{"q": {query}, "per_page": {githubPerPage}})
	if err != nil {
		return nil
	}
	var out []Issue
	for _, item := range asList(asMap(raw)["items"]) {
		m := asMap(item)
		out = append(out, Issue{
			Repo:  strings.TrimPrefix(asStr(m["repository_url"]), githubRepoPrefix),
			Title: asStr(m["title"]), URL: asStr(m["html_url"]), Number: asInt64(m["number"]),
			Pull: m["pull_request"] != nil, Updated: parseTime(m["updated_at"]),
		})
	}
	return out
}

// githubRepoPrefix precedes "owner/name" in a search item's repository_url.
const githubRepoPrefix = "https://api.github.com/repos/"

func loadRepo(ctx context.Context, api services.KeyedApi, name string) (GitRepo, error) {
	path := "repos/" + name
	info, err := api.Get(ctx, path, nil)
	if err != nil {
		return GitRepo{}, err
	}
	pulls, err := api.Get(ctx, path+"/pulls", url.Values{"state": {"open"}, "per_page": {githubPerPage}})
	if err != nil {
		return GitRepo{}, err
	}
	i := asMap(info)
	repo := GitRepo{Name: name, PRs: len(asList(pulls)), PushedAt: parseTime(i["pushed_at"])}
	// open_issues_count includes pull requests.
	repo.Issues = max(int(asFloat(i["open_issues_count"]))-repo.PRs, 0)

	runs, err := api.Get(ctx, path+"/actions/runs", url.Values{"branch": {asStr(i["default_branch"])}, "per_page": {"1"}})
	if err == nil {
		if list := asList(asMap(runs)["workflow_runs"]); len(list) > 0 {
			run := asMap(list[0])
			repo.CI, repo.CIURL = asStr(run["conclusion"]), asStr(run["html_url"])
			if repo.CI == ciFailure {
				repo.CIStep = failedStep(ctx, api, path+"/actions/runs/"+strconv.FormatInt(asInt64(run["id"]), 10)+"/jobs")
			}
		}
	}
	if release, err := api.Get(ctx, path+"/releases/latest", nil); err == nil && release != nil {
		r := asMap(release)
		repo.Release, repo.ReleasedAt = asStr(r["tag_name"]), parseTime(r["published_at"])
	}
	return repo, nil
}

// ciFailure is GitHub's conclusion of a failed run.
const ciFailure = "failure"

// failedStep names a run's first failed job and step: "test › go test";
// "" when the jobs cannot be read.
func failedStep(ctx context.Context, api services.KeyedApi, jobsPath string) string {
	jobs, err := api.Get(ctx, jobsPath, nil)
	if err != nil {
		return ""
	}
	for _, raw := range asList(asMap(jobs)["jobs"]) {
		job := asMap(raw)
		if asStr(job["conclusion"]) != ciFailure {
			continue
		}
		for _, st := range asList(job["steps"]) {
			if step := asMap(st); asStr(step["conclusion"]) == ciFailure {
				return asStr(job["name"]) + " › " + asStr(step["name"])
			}
		}
		return asStr(job["name"])
	}
	return ""
}

// ── Tibber ──

// PricePoint is one hour's electricity price (total incl. taxes).
type PricePoint struct {
	At     time.Time
	Total  float64
	Energy float64 // without grid fees and taxes
}

// EnergyDay is one day's consumption and cost.
type EnergyDay struct {
	Day       string
	KWh, Cost float64
	TempC     float64 // daily mean outside temperature
	HasTemp   bool
}

type TibberDataset struct {
	URL      string
	Home     string
	Currency string
	Current  float64
	// CurrentEnergy is the current price without grid fees and taxes.
	CurrentEnergy float64
	Level         string // VERY_CHEAP … VERY_EXPENSIVE
	Prices        []PricePoint
	Days          []EnergyDay
}

var TibberData = source{key: "tibber.data", ttl: priceTTL, service: enums.ServiceTibber, fetch: fetchTibber}

const tibberQuery = `{ viewer { homes { appNickname
  currentSubscription { priceInfo { current { total energy level currency }
    today { total energy startsAt } tomorrow { total energy startsAt } } }
  consumption(resolution: DAILY, last: 30) { nodes { from cost consumption } } } } }`

func fetchTibber(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTibber(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	body, err := services.BearerApi(sctx.URL, secret, sctx.TLS()).Post(ctx, "", map[string]any{"query": tibberQuery})
	if err != nil {
		return nil, fetchError(err)
	}
	b := asMap(body)
	if errs := asList(b["errors"]); len(errs) > 0 {
		return nil, newSourceError("%s", asStr(asMap(errs[0])["message"]))
	}
	homes := asList(asMap(asMap(b["data"])["viewer"])["homes"])
	if len(homes) == 0 {
		return nil, newSourceError("no home")
	}
	home := asMap(homes[0])
	info := asMap(asMap(home["currentSubscription"])["priceInfo"])
	current := asMap(info["current"])
	data := &TibberDataset{URL: sctx.URL, Home: asStr(home["appNickname"]), Currency: asStr(current["currency"]),
		Current: asFloat(current["total"]), CurrentEnergy: asFloat(current["energy"]), Level: asStr(current["level"])}
	for _, key := range []string{"today", "tomorrow"} {
		for _, raw := range asList(info[key]) {
			p := asMap(raw)
			data.Prices = append(data.Prices, PricePoint{At: parseTime(p["startsAt"]), Total: asFloat(p["total"]), Energy: asFloat(p["energy"])})
		}
	}
	for _, raw := range asList(asMap(home["consumption"])["nodes"]) {
		n := asMap(raw)
		data.Days = append(data.Days, EnergyDay{Day: day(n["from"]), KWh: asFloat(n["consumption"]), Cost: asFloat(n["cost"])})
	}
	addTemperatures(ctx, data.Days, sctx.Options)
	return data, nil
}

// addTemperatures fills the days' mean outside temperature from Open-Meteo
// when the connection has lat/lon options; best-effort.
func addTemperatures(ctx context.Context, days []EnergyDay, options map[string]any) {
	lat, lon := asFloat(options["lat"]), asFloat(options["lon"])
	if lat == 0 && lon == 0 {
		return
	}
	params := url.Values{"latitude": {fmtCoord(lat)}, "longitude": {fmtCoord(lon)}, "daily": {"temperature_2m_mean"},
		"past_days": {strconv.Itoa(tibberDays + 1)}, "forecast_days": {"1"}, "timezone": {"auto"}}
	body, _, err := httpclient.GetJSON(ctx, openMeteoURL, httpclient.Options{Params: params})
	if err != nil {
		return
	}
	daily := asMap(asMap(body)["daily"])
	temps := map[string]float64{}
	values := asList(daily["temperature_2m_mean"])
	for i, d := range asList(daily["time"]) {
		if i < len(values) && values[i] != nil {
			temps[asStr(d)] = asFloat(values[i])
		}
	}
	for i := range days {
		if t, ok := temps[days[i].Day]; ok {
			days[i].TempC, days[i].HasTemp = t, true
		}
	}
}

// ── Demo ──

func DemoVaultwarden(now time.Time) *VaultwardenDataset {
	data := &VaultwardenDataset{}
	demoworld.MustDecode("passwords", now, data)
	return data
}

func DemoGrocy(now time.Time) *GrocyDataset {
	data := &GrocyDataset{}
	demoworld.MustDecode("pantry", now, data)
	return data
}

func DemoDWD(now time.Time) *DWDDataset {
	data := &DWDDataset{}
	demoworld.MustDecode("weather", now, data)
	return data
}

func DemoGitHub(now time.Time) *GitHubDataset {
	data := &GitHubDataset{}
	demoworld.MustDecode("code.github", now, data)
	return data
}

func DemoTibber(now time.Time) *TibberDataset {
	var p struct {
		TibberDataset
		Price struct{ Base, Swing, GridAndTax float64 }
		Day   struct{ KWh, Cost, CostStep, TempC, TempStep float64 }
	}
	demoworld.MustDecode("energy", now, &p)
	data := p.TibberDataset

	// Local midnight: Truncate cuts at UTC midnight, which is 22:00 or
	// 23:00 the day before in Berlin.
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for h := range 48 { // today and tomorrow
		total := p.Price.Base + p.Price.Swing*float64((h+6)%24)/24
		data.Prices = append(data.Prices, PricePoint{At: start.Add(time.Duration(h) * time.Hour), Total: total, Energy: total - p.Price.GridAndTax})
	}
	data.Current, data.CurrentEnergy = data.Prices[now.Hour()].Total, data.Prices[now.Hour()].Energy
	for d := tibberDays; d > 0; d-- {
		step := float64(d % 5)
		data.Days = append(data.Days, EnergyDay{Day: now.AddDate(0, 0, -d).Format(time.DateOnly), KWh: p.Day.KWh + step,
			Cost: p.Day.Cost + step*p.Day.CostStep, TempC: p.Day.TempC - step*p.Day.TempStep, HasTemp: true})
	}
	return &data
}

func init() {
	Register(VaultwardenData)
	Register(testOf{VaultwardenData, func(d any) map[string]any { return map[string]any{"version": d.(*VaultwardenDataset).Version} }})
	Register(SpeedtestData)
	Register(SpeedResultsSource)
	Register(testOf{SpeedtestData, func(d any) map[string]any { return map[string]any{"down": d.(*SpeedtestDataset).Down} }})
	Register(GrocyData)
	Register(testOf{GrocyData, func(d any) map[string]any { return map[string]any{"missing": len(d.(*GrocyDataset).Missing)} }})
	Register(DWDData)
	Register(testOf{DWDData, func(d any) map[string]any { return map[string]any{"place": d.(*DWDDataset).Place} }})
	Register(GitHubData)
	Register(testOf{GitHubData, func(d any) map[string]any { return map[string]any{"repos": len(d.(*GitHubDataset).Repos)} }})
	Register(TibberData)
	Register(testOf{TibberData, func(d any) map[string]any { return map[string]any{"price": d.(*TibberDataset).Current} }})
}
