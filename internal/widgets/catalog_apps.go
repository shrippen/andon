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

func decodeDocker(raw map[string]any) any {
	return DockerConfig{OnlyProblems: asBool(raw["only_problems"])}
}

// DockerRow is one container line: state as tier, then the status text.
type DockerRow struct {
	Name, Image, Status, Tier string
}

// dockerView lists containers, problems first: unhealthy or crashed red,
// stopped cleanly grey.
func dockerView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(DockerConfig)
	data, ok := results["data"].(*sources.DockerDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeUmami(raw map[string]any) any { return UmamiConfig{Only: lowerList(raw["filter"])} }

// umamiView: visitors of the last 7 days per site, with the change
// against the week before (a drop in red).
func umamiView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(UmamiConfig)
	data, ok := results["data"].(*sources.UmamiDataset)
	if !ok {
		return map[string]any{}
	}
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

// immichView: library size, disk use, failed jobs and a pending update.
func immichView(_ any, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["data"].(*sources.ImmichDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeLinkwarden(raw map[string]any) any {
	return LinkwardenConfig{Only: lowerList(raw["filter"]), Newest: asBool(raw["newest"])}
}

func linkwardenView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(LinkwardenConfig)
	data, ok := results["data"].(*sources.LinkwardenDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeListOf(only string) func(map[string]any) any {
	return func(raw map[string]any) any {
		kind, _ := raw[only].(string)
		if kind == "all" {
			kind = ""
		}
		return PickConfig{Limit: clampInt(asInt(raw["limit"], listShown), 1, 20), Only: kind}
	}
}

func firstN[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}

func kintsugiView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(PickConfig)
	if !ok {
		cfg = PickConfig{Limit: listShown}
	}
	data, ok := results["data"].(*sources.KintsugiDataset)
	if !ok {
		return map[string]any{}
	}
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

func giteaView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(PickConfig)
	if !ok {
		cfg = PickConfig{Limit: listShown}
	}
	data, ok := results["data"].(*sources.GiteaDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeDawarich(raw map[string]any) any {
	return DawarichConfig{Yesterday: raw["day"] == "yesterday"}
}

func dawarichDayView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(DawarichConfig)
	data, ok := results["data"].(*sources.DawarichDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeAuthentik(raw map[string]any) any {
	return AuthentikConfig{Day: raw["period"] == "24h", OnlyFailures: asBool(raw["only_problems"])}
}

func authentikView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(AuthentikConfig)
	data, ok := results["data"].(*sources.AuthentikDataset)
	if !ok {
		return map[string]any{}
	}
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

func decodeVault(raw map[string]any) any { return VaultConfig{List: boolOr(raw["list_without"], true)} }

// vaultListed caps the accounts without 2FA named on the tile.
const vaultListed = 20

func vaultwardenView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(VaultConfig)
	if !ok {
		cfg.List = true
	}
	data, ok := results["data"].(*sources.VaultwardenDataset)
	if !ok {
		return map[string]any{}
	}
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

func init() {
	build := func(key string, service enums.ServiceType, refresh int, decode DecodeFunc, view ViewFunc, extra ...Query) WidgetType {
		return WidgetType{Key: key, Decode: decode, Template: "widgets/" + key, Category: CategoryInsight,
			Service: service, RefreshS: refresh, View: view,
			Queries: func(any) []Query { return append(dataQuery(nil), extra...) }}
	}
	on := func(key string, service enums.ServiceType, refresh int, decode DecodeFunc, view ViewFunc, extra ...Query) {
		Register(build(key, service, refresh, decode, view, extra...))
	}
	const minute, hour = 60, 3600

	on("kimai_week", enums.ServiceKimai, 10*minute, decodeKimaiWeek, kimaiWeekView)
	on("kimai_split", enums.ServiceKimai, 10*minute, decodeKimaiSplit, kimaiSplitView)
	on("unbilled_age", enums.ServiceKimai, hour, decodeAging([2]int{30, 60}), unbilledAgeView)
	on("disks", enums.ServiceScrutiny, hour, decodeDisks, disksView)
	on("komodo_stacks", enums.ServiceKomodo, 5*minute, decodeKomodo, komodoView)
	nas := build("truenas_pools", enums.ServiceTrueNAS, 10*minute, decodeTrueNAS, truenasView)
	nas.Extra = ExtraHistory // the pool forecast
	Register(nas)
	on("pihole", enums.ServicePihole, 5*minute, decodeDNS, dnsFilterView)
	on("adguard", enums.ServiceAdGuard, 5*minute, decodeDNS, dnsFilterView)
	on("vpn", enums.ServiceGluetun, 5*minute, decodeVPN, vpnView)
	on("gateway", enums.ServiceGateway, 5*minute, decodeGateway, gatewayView)
	on("expiry", enums.ServiceCerts, hour, decodeExpiry, expiryView,
		Query{Name: peerDomains, Source: "data", Conn: ConnPeer, Service: enums.ServiceDomains})
	on("sabnzbd", enums.ServiceSabnzbd, 5*minute, decodeSab, sabnzbdView)
	on("paperless_inbox", enums.ServicePaperless, 30*minute, decodePaperless, paperlessInboxView)
	mail := build("mail_invoices", enums.ServiceMail, hour, decodeMail, mailInvoicesView)
	mail.Extra = ExtraForwarded
	Register(mail)
	on("freshrss_feeds", enums.ServiceFreshRSS, 30*minute, decodeFreshRSS, freshrssView)
	on("docker_containers", enums.ServiceDocker, 5*minute, decodeDocker, dockerView)
	on("umami_sites", enums.ServiceUmami, 30*minute, decodeUmami, umamiView)
	on("immich_library", enums.ServiceImmich, 30*minute, decodeEmptyConfig, immichView)
	on("linkwarden", enums.ServiceLinkwarden, hour, decodeLinkwarden, linkwardenView)
	on("gitea_reviews", enums.ServiceGitea, 15*minute, decodeListOf("show"), giteaView)
	on("dawarich_day", enums.ServiceDawarich, 30*minute, decodeDawarich, dawarichDayView)
	on("authentik_logins", enums.ServiceAuthentik, 15*minute, decodeAuthentik, authentikView)
	on("vaultwarden_2fa", enums.ServiceVaultwarden, hour, decodeVault, vaultwardenView)
	on("kintsugi", enums.ServiceKintsugi, 15*minute, decodeListOf("kind"), kintsugiView)

	Register(WidgetType{Key: "speed_history", Decode: decodeSpeedHistory, Template: "widgets/speed_history", Category: CategoryInsight,
		Service: enums.ServiceSpeedtest, RefreshS: hour, View: speedHistoryView, Queries: dataQuery, Extra: ExtraHistory})
}
