package widgets

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// ── docker ──

// DockerConfig: all containers or only those with a problem.
type DockerConfig struct{ OnlyProblems bool }

func init() {
	Tile[DockerConfig]{Key: "docker_containers", Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceDocker, RefreshS: 5 * 60,
		Fields:  []Field{{Key: "only_problems", Input: InputCheck}},
		Decode:  func(r Raw) DockerConfig { return DockerConfig{OnlyProblems: r.Bool("only_problems")} },
		Queries: ownData[DockerConfig], View: dataView(dockerView)}.add()
}

// DockerRow is one container line: state as tier, then the status text.
type DockerRow struct {
	Name, Image, Status, Tier string
}

// dockerView lists containers, problems first: unhealthy or crashed red,
// stopped cleanly grey.
func dockerView(cfg DockerConfig, data *sources.DockerDataset, _ ViewCtx) map[string]any {
	var rows []DockerRow
	running := 0
	for _, c := range data.Containers {
		tier := "green"
		switch {
		case c.Health == sources.HealthUnhealthy, c.State == sources.StateRestarting,
			c.State == sources.StateExited && c.ExitCode != 0:
			tier = "red"
		case c.State != sources.StateRunning:
			tier = ""
		}
		if c.State == sources.StateRunning {
			running++
		}
		if cfg.OnlyProblems && tier != "red" {
			continue
		}
		rows = append(rows, DockerRow{Name: c.Name, Image: c.Image, Status: c.Status, Tier: tier})
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].Tier == "red" && rows[b].Tier != "red" })
	return map[string]any{"Rows": rows, "Running": running, "Total": len(data.Containers)}
}

// ── umami ──

// UmamiConfig filters the sites of the "umami_sites" tile.
type UmamiConfig struct{ Only []string }

func init() {
	Tile[UmamiConfig]{Key: "umami_sites", Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceUmami, RefreshS: 30 * 60,
		Fields:  []Field{{Key: "filter", Input: InputList}},
		Decode:  func(r Raw) UmamiConfig { return UmamiConfig{Only: r.Lower("filter")} },
		Queries: ownData[UmamiConfig], View: dataView(umamiView)}.add()
}

// umamiView: visitors of the last 7 days per site, with the change
// against the week before (a drop in red).
func umamiView(cfg UmamiConfig, data *sources.UmamiDataset, _ ViewCtx) map[string]any {
	var sites []sources.Site
	visitors := 0
	for _, s := range data.Sites {
		if matchesAny(s.Name, cfg.Only) || matchesAny(s.Domain, cfg.Only) {
			sites = append(sites, s)
			visitors += s.Visitors
		}
	}
	sort.Slice(sites, func(a, b int) bool { return sites[a].Visitors > sites[b].Visitors })
	top := 1
	if len(sites) > 0 {
		top = max(sites[0].Visitors, 1)
	}
	var bars []HBar
	for _, s := range sites[:min(len(sites), barsShown)] {
		bar := HBar{Label: s.Name, Value: strconv.Itoa(s.Visitors), W: pctOf(float64(s.Visitors), float64(top))}
		if s.PrevVisit > 0 {
			change := (s.Visitors - s.PrevVisit) * pctFull / s.PrevVisit
			bar.Value += fmt.Sprintf(" (%+d %%)", change)
			if change <= -umamiDrop {
				bar.Tier = "red"
			}
		}
		bars = append(bars, bar)
	}
	return map[string]any{"Visitors": visitors, "Sites": len(sites), "Bars": bars}
}

// umamiDrop marks a site whose visitors fell by this many percent.
const umamiDrop = 30

// ── immich ──

func init() {
	Tile[struct{}]{Key: "immich_library", Category: CategoryInsight, Topic: TopicHomelab, Service: enums.ServiceImmich, RefreshS: 30 * 60,
		Fields: []Field{}, Queries: ownData[struct{}], View: dataView(immichView)}.add()
}

// immichView: library size, disk use, failed jobs and a pending update.
func immichView(_ struct{}, data *sources.ImmichDataset, _ ViewCtx) map[string]any {
	failed := 0
	for _, n := range data.FailedJobs {
		failed += n
	}
	tier := ""
	switch {
	case data.DiskPercent >= immichDiskRed:
		tier = "red"
	case data.DiskPercent >= immichDiskYellow:
		tier = "yellow"
	}
	return map[string]any{"Data": data, "Photos": float64(data.Photos), "Failed": failed, "DiskTier": tier, "Disk": int(data.DiskPercent),
		"Update": data.Latest != "" && data.Latest != data.Version}
}

const (
	immichDiskYellow = 80
	immichDiskRed    = 90
)

// ── linkwarden ──

// LinkwardenConfig is the "linkwarden" widget's config.
type LinkwardenConfig struct {
	Only   []string // collection name parts (lower case), empty = all
	Newest bool     // list the latest links
}

// linksNewest is how many latest links the tile lists.
const linksNewest = 5

func init() {
	Tile[LinkwardenConfig]{Key: "linkwarden", Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceLinkwarden, RefreshS: 60 * 60,
		Fields: []Field{{Key: "filter", Input: InputList}, {Key: "newest", Input: InputCheck}},
		Decode: func(r Raw) LinkwardenConfig {
			return LinkwardenConfig{Only: r.Lower("filter"), Newest: r.Bool("newest")}
		},
		Queries: ownData[LinkwardenConfig], View: dataView(linkwardenView)}.add()
}

func linkwardenView(cfg LinkwardenConfig, data *sources.LinkwardenDataset, _ ViewCtx) map[string]any {
	counts := map[string]int{}
	var links []sources.Bookmark
	for _, l := range data.Links {
		if matchesAny(l.Collection, cfg.Only) {
			counts[l.Collection]++
			links = append(links, l)
		}
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(a, b int) bool { return counts[names[a]] > counts[names[b]] })
	top := 1
	if len(names) > 0 {
		top = counts[names[0]]
	}
	var bars []HBar
	for _, n := range names {
		if len(bars) >= barsShown {
			break
		}
		bars = append(bars, HBar{Label: n, Value: strconv.Itoa(counts[n]), W: pctOf(float64(counts[n]), float64(top))})
	}
	out := map[string]any{"Links": len(links), "Collections": len(names), "Bars": bars}
	if cfg.Newest {
		sort.SliceStable(links, func(a, b int) bool { return links[a].Created.After(links[b].Created) })
		out["Newest"] = firstN(links, linksNewest)
	}
	return out
}

// ── kintsugi ──

// PickConfig is a list tile's row count and which part it shows.
type PickConfig struct {
	Limit int
	Only  string // "" = everything
}

// pickAll is the select value for "everything".
const pickAll = "all"

// decodePick reads the row count and the select only.
func decodePick(only string) func(Raw) PickConfig {
	return func(r Raw) PickConfig {
		kind := r.Pick(only)
		if kind == pickAll {
			kind = ""
		}
		return PickConfig{Limit: r.Int("limit"), Only: kind}
	}
}

// pickLimit is the row count field of the list tiles.
var pickLimit = Field{Key: "limit", Input: InputNumber, Default: listShown, Min: "1", Max: "20"}

func init() {
	Tile[PickConfig]{Key: "kintsugi", Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceKintsugi, RefreshS: 15 * 60,
		Fields: []Field{pickLimit, sel("kind", pickAll, pickAll, "acquisition", "development")},
		Decode: decodePick("kind"), Queries: ownData[PickConfig], View: dataView(kintsugiView)}.add()
	Tile[PickConfig]{Key: "gitea_reviews", Category: CategoryInsight, Topic: TopicDev, Service: enums.ServiceGitea, RefreshS: 15 * 60,
		Fields: []Field{sel("show", pickAll, pickAll, "reviews", "issues"), pickLimit},
		Decode: decodePick("show"), Queries: ownData[PickConfig], View: dataView(giteaView)}.add()
}

func firstN[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}

func kintsugiView(cfg PickConfig, data *sources.KintsugiDataset, _ ViewCtx) map[string]any {
	var open []sources.KintsugiSuggestion
	for _, s := range data.Open {
		if cfg.Only == "" || string(s.Kind) == cfg.Only {
			open = append(open, s)
		}
	}
	failed := data.LastRun != nil && data.LastRun.Status == sources.KintsugiRunFailed
	return map[string]any{"Data": data, "Open": firstN(open, cfg.Limit), "RunFailed": failed}
}

// ── gitea_reviews ──

func giteaView(cfg PickConfig, data *sources.GiteaDataset, _ ViewCtx) map[string]any {
	// Head: reviews waiting, or the assigned issues when only those show.
	out := map[string]any{"Data": data, "Head": len(data.Reviews), "HeadKey": "gitea.reviews"}
	if cfg.Only == "issues" {
		out["Head"], out["HeadKey"] = len(data.Assigned), "gitea.assigned"
	}
	if cfg.Only != "issues" {
		out["Reviews"] = firstN(data.Reviews, cfg.Limit)
	}
	if cfg.Only != "reviews" {
		out["Assigned"] = firstN(data.Assigned, cfg.Limit)
	}
	return out
}

// ── dawarich_day ──

// dawarichTime reads a visit time: RFC3339 or Kimai-style offsets.
func dawarichTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// PlaceRow is one visit of today.
type PlaceRow struct {
	Name, From, To, Dur string
}

// DawarichConfig is the "dawarich_day" widget's config.
type DawarichConfig struct{ Yesterday bool }

func init() {
	Tile[DawarichConfig]{Key: "dawarich_day", Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceDawarich, RefreshS: 30 * 60,
		Fields:  []Field{sel("day", "today", "today", "yesterday")},
		Decode:  func(r Raw) DawarichConfig { return DawarichConfig{Yesterday: r.Pick("day") == "yesterday"} },
		Queries: ownData[DawarichConfig], View: dataView(dawarichDayView)}.add()
}

func dawarichDayView(cfg DawarichConfig, data *sources.DawarichDataset, ctx ViewCtx) map[string]any {
	now := time.Now()
	day := ctx.Today
	if cfg.Yesterday {
		y := todayOf(ctx).AddDate(0, 0, -1)
		day = y.Format(time.DateOnly)
		// The bar ends with that day; there is no "now" on it.
		now = time.Date(y.Year(), y.Month(), y.Day(), 23, 59, 0, 0, now.Location())
	}
	var spans []sources.KimaiSpan
	var rows []PlaceRow
	for _, v := range data.Visits {
		begin := dawarichTime(v.Start)
		if begin.IsZero() || begin.In(now.Location()).Format(time.DateOnly) != day {
			continue
		}
		end := dawarichTime(v.End)
		spans = append(spans, sources.KimaiSpan{Begin: begin, End: end})
		to := ""
		if !end.IsZero() {
			to = end.In(now.Location()).Format("15:04")
		}
		rows = append(rows, PlaceRow{Name: v.Name, From: begin.In(now.Location()).Format("15:04"), To: to, Dur: clockMinutes(v.Minutes)})
	}
	from, to, segs, pos := dayBar(spans, now)
	out := map[string]any{"Rows": rows, "DaySegs": segs, "DayNow": pos, "DayTicks": dayTicks(from, to), "Yesterday": cfg.Yesterday}
	if cfg.Yesterday {
		out["DayNow"] = nil
	}
	return out
}

// ── authentik_logins ──

// AuthentikConfig is the "authentik_logins" widget's config.
type AuthentikConfig struct {
	Day          bool // the last 24 hours instead of 7 days
	OnlyFailures bool
}

func init() {
	Tile[AuthentikConfig]{Key: "authentik_logins", Category: CategoryInsight, Topic: TopicSecurity, Service: enums.ServiceAuthentik, RefreshS: 15 * 60,
		Fields:  []Field{sel("period", "7d", "24h", "7d"), {Key: "only_problems", Input: InputCheck}},
		Renames: []rename{{from: "only_failures", to: "only_problems"}, {from: "span", to: "period"}},
		Decode: func(r Raw) AuthentikConfig {
			return AuthentikConfig{Day: r.Pick("period") == "24h", OnlyFailures: r.Bool("only_problems")}
		},
		Queries: ownData[AuthentikConfig], View: dataView(authentikView)}.add()
}

func authentikView(cfg AuthentikConfig, data *sources.AuthentikDataset, _ ViewCtx) map[string]any {
	span := 7 * 24 * time.Hour
	logins, failed := data.Logins7d, data.Failed7d
	if cfg.Day {
		span, logins, failed = 24*time.Hour, data.Logins24h, data.Failed24h
	}
	list := data.Logins
	if cfg.OnlyFailures {
		list = data.Failures
	}
	since := time.Now().Add(-span)
	var shown []sources.AKLogin
	for _, l := range list {
		if l.At.After(since) && len(shown) < listShown {
			shown = append(shown, l)
		}
	}
	return map[string]any{"Data": data, "Logins": shown, "Count": logins, "Failed": failed, "Day": cfg.Day, "OnlyFailures": cfg.OnlyFailures}
}

// ── vaultwarden_2fa ──

// VaultConfig is the "vaultwarden_2fa" widget's config.
type VaultConfig struct{ List bool }

func init() {
	Tile[VaultConfig]{Key: "vaultwarden_2fa", Category: CategoryInsight, Topic: TopicSecurity, Service: enums.ServiceVaultwarden, RefreshS: 60 * 60,
		Fields:  []Field{{Key: "list_without", Input: InputCheck, Default: true}},
		Decode:  func(r Raw) VaultConfig { return VaultConfig{List: r.Bool("list_without")} },
		Queries: ownData[VaultConfig], View: dataView(vaultwardenView)}.add()
}

// vaultListed caps the accounts without 2FA named on the tile.
const vaultListed = 20

func vaultwardenView(cfg VaultConfig, data *sources.VaultwardenDataset, _ ViewCtx) map[string]any {
	with, active := 0, 0
	var without []string
	for _, u := range data.Users {
		if !u.Enabled {
			continue
		}
		active++
		if u.TwoFactor {
			with++
		} else if cfg.List && len(without) < vaultListed {
			without = append(without, u.Email)
		}
	}
	return map[string]any{"With": with, "Active": active, "Without": without, "WithPct": pctOf(float64(with), float64(max(active, 1))),
		"Version": data.Version}
}
