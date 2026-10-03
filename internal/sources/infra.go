package sources

// TrueNAS (storage), Komodo (containers), Pangolin (tunnels) and
// authentik (identity: usage statistics).

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	pangolinPage   = "100"
	authentikPage  = 100
	authentikPages = 5
	authentikDays  = "7"
)

// ── TrueNAS ──

type Pool struct {
	Name, Status    string
	Healthy         bool
	Size, Allocated float64
	ScrubEnd        time.Time // last finished scrub, zero = none known
	ScrubErrors     int
}

type TNAlert struct {
	ID, Level, Text string
}

type TNApp struct {
	Name, State string
	Update      bool
}

// SnapTask is a periodic snapshot task and its last run.
type SnapTask struct {
	Dataset, State string // State: FINISHED, RUNNING, ERROR, PENDING
	Enabled        bool
	Last           time.Time
}

type TrueNASDataset struct {
	URL, Host, Version string
	Pools              []Pool
	Alerts             []TNAlert // not dismissed
	Apps               []TNApp
	Snapshots          []SnapTask
}

var TrueNASData = source{key: "truenas.data", ttl: opsTTL, service: enums.ServiceTrueNAS, fetch: fetchTrueNAS}

func fetchTrueNAS(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTrueNAS(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	// TrueNAS revokes an API key that was sent over plain HTTP: never do that.
	if !strings.HasPrefix(strings.ToLower(sctx.URL), "https://") {
		return nil, newSourceError("truenas.https_required")
	}
	session, err := services.TrueNASApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}.Open(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	defer session.Close()

	data := &TrueNASDataset{URL: sctx.URL}
	info, err := session.Call(ctx, "system.info")
	if err != nil {
		return nil, fetchError(err)
	}
	data.Host, data.Version = asStr(asMap(info)["hostname"]), asStr(asMap(info)["version"])
	pools, err := session.Call(ctx, "pool.query")
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(pools) {
		p := asMap(raw)
		pool := Pool{Name: asStr(p["name"]), Status: asStr(p["status"]), Healthy: asBool(p["healthy"]),
			Size: asFloat(p["size"]), Allocated: asFloat(p["allocated"])}
		if scan := asMap(p["scan"]); asStr(scan["function"]) == "SCRUB" && asStr(scan["state"]) == "FINISHED" {
			pool.ScrubEnd = time.UnixMilli(asInt64(asMap(scan["end_time"])["$date"])).UTC()
			pool.ScrubErrors = int(asFloat(scan["errors"]))
		}
		data.Pools = append(data.Pools, pool)
	}
	if alerts, err := session.Call(ctx, "alert.list"); err == nil {
		for _, raw := range asList(alerts) {
			a := asMap(raw)
			if asBool(a["dismissed"]) {
				continue
			}
			data.Alerts = append(data.Alerts, TNAlert{ID: asStr(a["uuid"]), Level: asStr(a["level"]), Text: strings.TrimSpace(asStr(a["formatted"]))})
		}
	}
	if tasks, err := session.Call(ctx, "pool.snapshottask.query"); err == nil {
		for _, raw := range asList(tasks) {
			t := asMap(raw)
			state := asMap(t["state"])
			data.Snapshots = append(data.Snapshots, SnapTask{Dataset: asStr(t["dataset"]), Enabled: asBool(t["enabled"]),
				State: asStr(state["state"]), Last: time.UnixMilli(asInt64(asMap(state["datetime"])["$date"])).UTC()})
		}
	}
	// Apps exist on SCALE only.
	if apps, err := session.Call(ctx, "app.query"); err == nil {
		for _, raw := range asList(apps) {
			a := asMap(raw)
			data.Apps = append(data.Apps, TNApp{Name: asStr(a["name"]), State: asStr(a["state"]), Update: asBool(a["upgrade_available"])})
		}
	}
	return data, nil
}

// TNDataset is one dataset's use, for the dialog's "who fills the pool".
type TNDataset struct {
	Name            string // "tank/photos"
	Used, Available float64
}

// TrueNASDatasets are the datasets, largest first, fetched on open.
type TrueNASDatasets struct{ List []TNDataset }

var TrueNASDatasetsSource = source{key: "truenas.datasets", ttl: detailTTL, service: enums.ServiceTrueNAS, fetch: fetchTrueNASDatasets}

func fetchTrueNASDatasets(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTrueNASDatasets(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.ToLower(sctx.URL), "https://") {
		return nil, newSourceError("truenas.https_required")
	}
	session, err := services.TrueNASApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}.Open(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	defer session.Close()
	raw, err := session.Call(ctx, "pool.dataset.query")
	if err != nil {
		return nil, fetchError(err)
	}
	return parseTNDatasets(raw), nil
}

// parseTNDatasets keeps the datasets below a pool's root, largest first.
func parseTNDatasets(raw any) *TrueNASDatasets {
	out := &TrueNASDatasets{}
	for _, item := range asList(raw) {
		d := asMap(item)
		name := asStr(d["name"])
		if !strings.Contains(name, "/") {
			continue
		}
		out.List = append(out.List, TNDataset{Name: name, Used: asFloat(asMap(d["used"])["parsed"]), Available: asFloat(asMap(d["available"])["parsed"])})
	}
	sort.SliceStable(out.List, func(a, b int) bool { return out.List[a].Used > out.List[b].Used })
	return out
}

// ── Komodo ──

type KStack struct {
	Name, State string
	Updates     []string // services with a newer image
}

type KAlert struct {
	Level, Kind, Name string
	At                time.Time
}

type KomodoDataset struct {
	URL                                          string
	ServersTotal, ServersHealthy, ServersProblem int
	Stacks                                       []KStack
	Alerts                                       []KAlert // open
}

var KomodoData = source{key: "komodo.data", ttl: opsTTL, service: enums.ServiceKomodo, fetch: fetchKomodo}

func fetchKomodo(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKomodo(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KomodoApi{URL: sctx.URL, Secret: secret, Verify: sctx.VerifyTLS}
	servers, err := api.Read(ctx, "GetServersSummary", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	s := asMap(servers)
	data := &KomodoDataset{URL: sctx.URL, ServersTotal: int(asFloat(s["total"])), ServersHealthy: int(asFloat(s["healthy"])),
		ServersProblem: int(asFloat(s["warning"]) + asFloat(s["unhealthy"]))}

	stacks, err := api.Read(ctx, "ListStacks", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(stacks) {
		st := asMap(raw)
		info := asMap(st["info"])
		stack := KStack{Name: asStr(st["name"]), State: strings.ToLower(asStr(info["state"]))}
		for _, svc := range asList(info["services"]) {
			if sv := asMap(svc); asBool(sv["update_available"]) {
				stack.Updates = append(stack.Updates, asStr(sv["service"]))
			}
		}
		data.Stacks = append(data.Stacks, stack)
	}

	alerts, err := api.Read(ctx, "ListAlerts", map[string]any{"query": map[string]any{"resolved": false}})
	if err == nil {
		for _, raw := range asList(asMap(alerts)["alerts"]) {
			a := asMap(raw)
			body := asMap(a["data"])
			data.Alerts = append(data.Alerts, KAlert{Level: strings.ToUpper(asStr(a["level"])), Kind: asStr(body["type"]),
				Name: asStr(asMap(body["data"])["name"]), At: time.UnixMilli(asInt64(a["ts"])).UTC()})
		}
	}
	return data, nil
}

// KServerLoad is one Komodo server's load when the dialog opens.
type KServerLoad struct {
	Name              string
	State             string  // ok, warning, unhealthy, …
	CPU               float64 // percent
	MemUsed, MemTotal float64 // GB
	DiskUsed, DiskMax float64 // GB, all disks
}

// KDeploy is a stack's latest deployment.
type KDeploy struct {
	At        time.Time
	Operation string // DeployStack, …
	By        string
	OK        bool
}

// KomodoDetail is what the dialog fetches on open.
type KomodoDetail struct {
	Servers []KServerLoad
	Deploys map[string]KDeploy // by stack name
}

var KomodoDetailSource = source{key: "komodo.detail", ttl: detailTTL, service: enums.ServiceKomodo, fetch: fetchKomodoDetail}

// komodoParallel is how many servers are asked at once.
const komodoParallel = 4

func fetchKomodoDetail(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKomodoDetail(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KomodoApi{URL: sctx.URL, Secret: secret, Verify: sctx.VerifyTLS}
	servers, err := api.Read(ctx, "ListServers", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	list := asList(servers)
	out := &KomodoDetail{Servers: make([]KServerLoad, len(list)), Deploys: map[string]KDeploy{}}
	parallel(ctx, len(list), komodoParallel, func(i int) {
		s := asMap(list[i])
		load := KServerLoad{Name: asStr(s["name"]), State: strings.ToLower(asStr(asMap(s["info"])["state"]))}
		if stats, err := api.Read(ctx, "GetSystemStats", map[string]any{"server": asStr(s["id"])}); err == nil {
			m := asMap(stats)
			load.CPU, load.MemUsed, load.MemTotal = asFloat(m["cpu_perc"]), asFloat(m["mem_used_gb"]), asFloat(m["mem_total_gb"])
			for _, d := range asList(m["disks"]) {
				load.DiskUsed += asFloat(asMap(d)["used_gb"])
				load.DiskMax += asFloat(asMap(d)["total_gb"])
			}
		}
		out.Servers[i] = load
	})

	stacks, err := api.Read(ctx, "ListStacks", nil)
	if err != nil {
		return out, nil
	}
	names := map[string]string{}
	for _, raw := range asList(stacks) {
		names[asStr(asMap(raw)["id"])] = asStr(asMap(raw)["name"])
	}
	// Newest first: the first update of a stack is its latest.
	updates, err := api.Read(ctx, "ListUpdates", map[string]any{"query": map[string]any{"target.type": "Stack"}})
	if err != nil {
		return out, nil
	}
	for _, raw := range asList(asMap(updates)["updates"]) {
		u := asMap(raw)
		name := names[asStr(asMap(u["target"])["id"])]
		if _, seen := out.Deploys[name]; name == "" || seen {
			continue
		}
		out.Deploys[name] = KDeploy{At: time.UnixMilli(asInt64(u["start_ts"])).UTC(), Operation: asStr(u["operation"]),
			By: asStr(u["username"]), OK: asBool(u["success"])}
	}
	return out, nil
}

// ── Pangolin ──

type PSite struct {
	Name, Type  string
	Online      *bool // nil for local sites
	MBIn, MBOut float64
	Update      bool
}

type PResource struct {
	ID                   int64
	Name, Domain, Health string
	Enabled              bool
	SSO                  bool // behind Pangolin's login (or an identity provider)
}

type PangolinDataset struct {
	URL, Org  string
	Sites     []PSite
	Resources []PResource
}

var PangolinData = source{key: "pangolin.data", ttl: opsTTL, service: enums.ServicePangolin, fetch: fetchPangolin}

// Fetch needs options.org, the organisation id.
func fetchPangolin(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoPangolin(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	org := asStr(sctx.Options["org"])
	if org == "" {
		return nil, newSourceError("options: org missing")
	}
	// The dashboard address answers with HTML; the integration API ends in /v1.
	if !strings.HasSuffix(strings.TrimRight(sctx.URL, "/"), "/v1") {
		return nil, newSourceError("pangolin.url")
	}
	api := services.PangolinApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}
	base := "org/" + url.PathEscape(org) + "/"
	page := url.Values{"pageSize": {pangolinPage}}

	sites, err := api.Get(ctx, base+"sites", page)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &PangolinDataset{URL: sctx.URL, Org: org}
	for _, raw := range asList(asMap(sites)["sites"]) {
		s := asMap(raw)
		site := PSite{Name: asStr(s["name"]), Type: asStr(s["type"]), MBIn: asFloat(s["megabytesIn"]), MBOut: asFloat(s["megabytesOut"]),
			Update: asBool(s["newtUpdateAvailable"])}
		if online, ok := s["online"].(bool); ok {
			site.Online = &online
		}
		data.Sites = append(data.Sites, site)
	}
	resources, err := api.Get(ctx, base+"resources", page)
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(asMap(resources)["resources"]) {
		r := asMap(raw)
		health := asStr(r["healthStatus"])
		if health == "" {
			health = asStr(r["health"])
		}
		data.Resources = append(data.Resources, PResource{ID: asInt64(r["resourceId"]), Name: asStr(r["name"]), Domain: asStr(r["fullDomain"]),
			Enabled: asBool(r["enabled"]), Health: health, SSO: asBool(r["sso"])})
	}
	return data, nil
}

// PAccess is one resource's requests of the last week (Pangolin's
// request audit log).
type PAccess struct {
	Requests, Blocked int
	Countries         []Count // the top ones
}

// PangolinAccess is the dialog's extra per resource name.
type PangolinAccess struct{ ByResource map[string]PAccess }

var PangolinAccessSource = source{key: "pangolin.access", ttl: detailTTL, service: enums.ServicePangolin, fetch: fetchPangolinAccess}

// Limits of the access fetch.
const (
	pangolinAccessMax = 12
	pangolinCountries = 3
)

func fetchPangolinAccess(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return &PangolinAccess{ByResource: map[string]PAccess{
			"Immich": {Requests: 18420, Blocked: 12, Countries: []Count{{"DE", 18100}, {"NL", 240}}},
			"Gitea":  {Requests: 2210, Blocked: 960, Countries: []Count{{"DE", 1100}, {"US", 620}, {"CN", 410}}},
		}}, nil
	}
	data, err := fetchPangolin(ctx, sctx)
	if err != nil {
		return nil, err
	}
	p := data.(*PangolinDataset)
	secret, _ := needSecret(sctx)
	api := services.PangolinApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}
	out := &PangolinAccess{ByResource: map[string]PAccess{}}
	var mu sync.Mutex
	list := firstOf(p.Resources, pangolinAccessMax)
	parallel(ctx, len(list), releasesParallel, func(i int) {
		r := list[i]
		if !r.Enabled || r.ID == 0 {
			return
		}
		body, err := api.Get(ctx, "org/"+url.PathEscape(p.Org)+"/logs/analytics", url.Values{"resourceId": {strconv.FormatInt(r.ID, 10)}})
		if err != nil {
			return
		}
		m := asMap(body)
		acc := PAccess{Requests: int(asFloat(m["totalRequests"])), Blocked: int(asFloat(m["totalBlocked"]))}
		for _, c := range firstOf(asList(m["requestsPerCountry"]), pangolinCountries) {
			acc.Countries = append(acc.Countries, Count{Name: asStr(asMap(c)["code"]), N: int(asFloat(asMap(c)["count"]))})
		}
		mu.Lock()
		out.ByResource[r.Name] = acc
		mu.Unlock()
	})
	return out, nil
}

// ── authentik ──

// AKApp is one application's authorizations in the last 7 days.
type AKApp struct {
	Name          string
	Events, Users int
}

// AKLogin is one successful login with the place authentik resolved.
type AKLogin struct {
	User, IP, Country, City string
	Lat, Lon                float64 // 0, 0 when unknown
	At                      time.Time
}

type AKUser struct {
	Name      string
	LastLogin time.Time // zero: never
}

type AuthentikDataset struct {
	URL, Version, Latest string
	Outdated, Outposts   bool // update for server / outposts
	Logins7d, Failed7d   int
	Logins24h, Failed24h int
	Apps                 []AKApp
	Users                []AKUser  // active human accounts
	Logins               []AKLogin // latest logins, newest first
	Failures             []AKLogin // latest failed logins, newest first
	Days                 []AKDay   // logins per day of the week, oldest first
}

// AKDay is one day's logins and failed logins.
type AKDay struct {
	Day            string // "2026-09-27"
	Logins, Failed int
}

var AuthentikData = source{key: "authentik.data", ttl: opsTTL, service: enums.ServiceAuthentik, fetch: fetchAuthentik}

// authentikLogins reads the latest events of one login action; a failed
// login names the attempted user in its context. Best-effort.
func authentikLogins(ctx context.Context, api services.AuthentikApi, action string) []AKLogin {
	events, err := api.Get(ctx, "events/events/", url.Values{"action": {action}, "ordering": {"-created"}, "page_size": {strconv.Itoa(authentikPage)}})
	if err != nil {
		return nil
	}
	var out []AKLogin
	for _, raw := range asList(asMap(events)["results"]) {
		e := asMap(raw)
		detail := asMap(e["context"])
		geo := asMap(detail["geo"])
		at, _ := time.Parse(time.RFC3339, asStr(e["created"]))
		user := asStr(detail["username"])
		if user == "" {
			user = asStr(asMap(e["user"])["username"])
		}
		out = append(out, AKLogin{User: user, IP: asStr(e["client_ip"]), Country: asStr(geo["country"]), City: asStr(geo["city"]),
			Lat: asFloat(geo["lat"]), Lon: asFloat(geo["long"]), At: at.UTC()})
	}
	return out
}

func fetchAuthentik(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoAuthentik(time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.AuthentikApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}
	data, err := loadAuthentik(ctx, api, sctx.URL, time.Now().UTC())
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadAuthentik(ctx context.Context, api services.AuthentikApi, base string, now time.Time) (*AuthentikDataset, error) {
	version, err := api.Get(ctx, "admin/version/", nil)
	if err != nil {
		return nil, err
	}
	v := asMap(version)
	data := &AuthentikDataset{URL: base, Version: asStr(v["version_current"]), Latest: asStr(v["version_latest"]),
		Outdated: asBool(v["outdated"]), Outposts: asBool(v["outpost_outdated"])}

	// Event volume in 6-hour buckets over 7 days.
	volume, err := api.Get(ctx, "events/events/volume/", url.Values{"history_days": {authentikDays}})
	if err != nil {
		return nil, err
	}
	days := map[string]*AKDay{}
	dayOf := func(at time.Time) *AKDay {
		key := at.Format(time.DateOnly)
		if days[key] == nil {
			days[key] = &AKDay{Day: key}
		}
		return days[key]
	}
	defer func() {
		for _, d := range days {
			data.Days = append(data.Days, *d)
		}
		sort.Slice(data.Days, func(a, b int) bool { return data.Days[a].Day < data.Days[b].Day })
	}()
	for _, raw := range asList(volume) {
		b := asMap(raw)
		n := int(asFloat(b["count"]))
		at, _ := time.Parse(time.RFC3339, asStr(b["time"]))
		switch asStr(b["action"]) {
		case "login":
			dayOf(at).Logins += n
			data.Logins7d += n
			if now.Sub(at) <= 24*time.Hour {
				data.Logins24h += n
			}
		case "login_failed":
			dayOf(at).Failed += n
			data.Failed7d += n
			if now.Sub(at) <= 24*time.Hour {
				data.Failed24h += n
			}
		}
	}

	// Latest logins with their place (authentik adds GeoIP to events).
	data.Logins = authentikLogins(ctx, api, "login")
	data.Failures = authentikLogins(ctx, api, "login_failed")

	if top, err := api.Get(ctx, "events/events/top_per_user/", url.Values{"action": {"authorize_application"}, "top_n": {"10"}}); err == nil {
		for _, raw := range asList(top) {
			t := asMap(raw)
			data.Apps = append(data.Apps, AKApp{Name: asStr(asMap(t["application"])["name"]),
				Events: int(asFloat(t["counted_events"])), Users: int(asFloat(t["unique_users"]))})
		}
	}

	for page := 1; page <= authentikPages; page++ {
		users, err := api.Get(ctx, "core/users/", url.Values{"is_active": {"true"}, "page_size": {strconv.Itoa(authentikPage)}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		body := asMap(users)
		for _, raw := range asList(body["results"]) {
			u := asMap(raw)
			if strings.Contains(asStr(u["type"]), "service_account") {
				continue
			}
			name := asStr(u["username"])
			last, _ := time.Parse(time.RFC3339, asStr(u["last_login"]))
			data.Users = append(data.Users, AKUser{Name: name, LastLogin: last.UTC()})
		}
		if asStr(asMap(body["pagination"])["next"]) == "" && asFloat(asMap(body["pagination"])["next"]) == 0 {
			break
		}
	}
	sort.Slice(data.Apps, func(i, j int) bool { return data.Apps[i].Events > data.Apps[j].Events })
	return data, nil
}

func init() {
	Register(TrueNASData)
	Register(testOf{TrueNASData, func(d any) map[string]any {
		t := d.(*TrueNASDataset)
		return map[string]any{"version": t.Version, "pools": len(t.Pools)}
	}})
	Register(KomodoData)
	Register(KomodoDetailSource)
	Register(PangolinAccessSource)
	Register(TrueNASDatasetsSource)
	Register(testOf{KomodoData, func(d any) map[string]any { return map[string]any{"stacks": len(d.(*KomodoDataset).Stacks)} }})
	Register(PangolinData)
	Register(testOf{PangolinData, func(d any) map[string]any { return map[string]any{"sites": len(d.(*PangolinDataset).Sites)} }})
	Register(AuthentikData)
	Register(testOf{AuthentikData, func(d any) map[string]any { return map[string]any{"version": d.(*AuthentikDataset).Version} }})
}
