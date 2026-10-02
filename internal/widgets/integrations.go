package widgets

// Widgets of the network, media and everyday integrations:
//
//	tailscale     devices with online state and key expiry
//	mediaserver   running streams and library size
//	arr_upcoming  next episodes / movies of Sonarr or Radarr
//	grocy         expired and due products, chores
//	dwd           weather warnings
//	github        repos: PRs, issues, CI, latest release
//	speedtest     latest down/up/ping
//	energy        Tibber prices, cheapest hours, cost; power from Home Assistant

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	peerHass       = "hass"
	upcomingShown  = 8
	integrationTTL = 300 // seconds between reloads
	energyDaysCost = 30
)

// EnergyConfig: an optional Home Assistant sensor with the current power.
type EnergyConfig struct {
	PowerEntity string
	CheapHours  int  // length of the cheap window
	Tomorrow    bool // the curve runs into tomorrow when known
	EnergyOnly  bool // prices without grid fees and taxes
}

func decodeEnergy(r Raw) EnergyConfig {
	return EnergyConfig{PowerEntity: r.String("power_entity"), CheapHours: r.Int("cheap_hours"),
		Tomorrow: r.Bool("tomorrow"), EnergyOnly: r.Pick("price") == "energy"}
}

func energyQueries(cfg EnergyConfig) []Query {
	queries := dataQuery(nil)
	if cfg.PowerEntity != "" {
		queries = append(queries, Query{Name: peerHass, Source: "data", Conn: ConnPeer, Service: enums.ServiceHomeAssistant})
	}
	return queries
}

// GrocyConfig is the "grocy" widget's config.
type GrocyConfig struct {
	Hide map[string]bool // stock, shopping, chores
	Days int             // soon and chores within this many days, 0 = as Grocy says (5 days, all chores)
}

var grocyParts = []string{"stock", "shopping", "chores"}

func decodeGrocy(r Raw) GrocyConfig {
	cfg := GrocyConfig{Hide: map[string]bool{}, Days: r.Int("days")}
	for _, p := range grocyParts {
		if !r.Bool("show_" + p) {
			cfg.Hide[p] = true
		}
	}
	return cfg
}

func grocyView(cfg GrocyConfig, data *sources.GrocyDataset, _ ViewCtx) map[string]any {
	shown := *data
	if cfg.Hide["stock"] {
		shown.Expired, shown.Overdue, shown.Soon = nil, nil, nil
	}
	if cfg.Hide["shopping"] {
		shown.Missing = nil
	}
	if cfg.Hide["chores"] {
		shown.Chores = nil
	}
	if !cfg.Hide["stock"] {
		shown.Soon = data.SoonWithin(cmp.Or(cfg.Days, sources.GrocySoonDays), time.Now())
	}
	if cfg.Days > 0 {
		until := time.Now().AddDate(0, 0, cfg.Days)
		var chores []sources.Chore
		for _, c := range shown.Chores {
			if c.Due.Before(until) {
				chores = append(chores, c)
			}
		}
		shown.Chores = chores
	}
	return map[string]any{"Data": &shown}
}

// DWDConfig is the "dwd" widget's config.
type DWDConfig struct{ MinLevel int }

// dwdLevels ranks DWD severities.
var dwdLevels = map[string]int{"minor": 1, "moderate": 2, "severe": 3, "extreme": 4}

func decodeDWD(r Raw) DWDConfig { return DWDConfig{MinLevel: dwdLevels[r.Pick("min_level")]} }

func dwdView(cfg DWDConfig, data *sources.DWDDataset, _ ViewCtx) map[string]any {
	shown := *data
	shown.Warnings = nil
	for _, w := range data.Warnings {
		// Unknown severities stay: better one warning too many.
		if rank, known := dwdLevels[strings.ToLower(w.Severity)]; !known || rank >= cfg.MinLevel {
			shown.Warnings = append(shown.Warnings, w)
		}
	}
	return map[string]any{"Data": &shown}
}

// TailscaleConfig is the "tailscale" widget's config.
type TailscaleConfig struct {
	OnlyTrouble bool     // offline, or the key runs out within tailKeyDays
	Tags        []string // any of these tags ("server" or "tag:server"), empty = all
	HideAfter   int      // hide devices offline longer than this many days, 0 = show all
}

// tailKeyDays is when an expiring key counts as trouble.
const tailKeyDays = 14

func decodeTailscale(r Raw) TailscaleConfig {
	var tags []string
	for _, t := range r.Lower("tags") {
		if !strings.HasPrefix(t, "tag:") {
			t = "tag:" + t
		}
		tags = append(tags, t)
	}
	return TailscaleConfig{OnlyTrouble: r.Bool("only_problems"), Tags: tags, HideAfter: r.Int("hide_after")}
}

func tailscaleView(cfg TailscaleConfig, data *sources.TailscaleDataset, _ ViewCtx) map[string]any {
	now := time.Now()
	soon := now.AddDate(0, 0, tailKeyDays)
	shown := *data
	shown.Devices = nil
	for _, d := range data.Devices {
		if len(cfg.Tags) > 0 && !slices.ContainsFunc(d.Tags, func(t string) bool { return slices.Contains(cfg.Tags, strings.ToLower(t)) }) {
			continue
		}
		if cfg.HideAfter > 0 && !d.Online && !d.LastSeen.IsZero() && d.LastSeen.Before(now.AddDate(0, 0, -cfg.HideAfter)) {
			continue
		}
		trouble := !d.Online || (!d.KeyExpiry.IsZero() && d.KeyExpiry.Before(soon))
		if cfg.OnlyTrouble && !trouble {
			continue
		}
		shown.Devices = append(shown.Devices, d)
	}
	return map[string]any{"Data": &shown, "OnlyTrouble": cfg.OnlyTrouble, "Total": len(data.Devices), "Now": now, "Soon": soon}
}

// GitHubConfig is the "github" widget's config.
type GitHubConfig struct {
	Only  []string // repo name parts (lower case), empty = all
	RedCI bool     // only repos whose CI failed
}

func decodeGitHub(r Raw) GitHubConfig {
	return GitHubConfig{Only: r.Lower("filter"), RedCI: r.Bool("only_problems")}
}

func githubView(cfg GitHubConfig, data *sources.GitHubDataset, _ ViewCtx) map[string]any {
	shown := *data
	shown.Repos = nil
	for _, r := range data.Repos {
		if matchesAny(r.Name, cfg.Only) && (!cfg.RedCI || r.CI == "failure") {
			shown.Repos = append(shown.Repos, r)
		}
	}
	return map[string]any{"Data": &shown, "RedCI": cfg.RedCI}
}

// ArrConfig is the "arr_upcoming" widget's config.
type ArrConfig struct{ Days int }

// arrDays is the default look ahead of the Arr tile.
const arrDays = 7

func arrView(cfg ArrConfig, data *sources.ArrDataset, _ ViewCtx) map[string]any {
	until := time.Now().AddDate(0, 0, cfg.Days)
	var items []sources.ArrItem
	for _, it := range data.Upcoming {
		if it.At.Before(until) && len(items) < upcomingShown {
			items = append(items, it)
		}
	}
	return map[string]any{"Data": data, "Items": items, "Kind": arrKinds[data.App]}
}

// arrKinds names what an *arr app lists, for the tile's head line
// ("Sonarr · Episoden"); catalog keys arr.kind_<kind>.
var arrKinds = map[string]string{"Sonarr": "episodes", "Radarr": "movies", "Lidarr": "albums", "Readarr": "books"}

// MediaConfig is the "mediaserver" widget's config.
type MediaConfig struct{ Users bool }

func mediaView(cfg MediaConfig, results map[string]any, _ ViewCtx) map[string]any {
	if results["data"] == nil {
		return map[string]any{}
	}
	return map[string]any{"Data": results["data"], "Users": cfg.Users}
}

// energyPrices is the dataset as the tile shows it: without tomorrow
// unless wanted, and with the energy-only price as "total" if asked.
func energyPrices(raw *sources.TibberDataset, cfg EnergyConfig, now time.Time) *sources.TibberDataset {
	data := *raw
	endOfToday := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	data.Prices = nil
	for _, p := range raw.Prices {
		if !cfg.Tomorrow && !p.At.Before(endOfToday) {
			continue
		}
		if cfg.EnergyOnly {
			p.Total = p.Energy
		}
		data.Prices = append(data.Prices, p)
	}
	if cfg.EnergyOnly {
		data.Current = raw.CurrentEnergy
	}
	return &data
}

// energyView draws today's and tomorrow's prices and finds the cheapest
// hours ahead.
func energyView(cfg EnergyConfig, results map[string]any, _ ViewCtx) map[string]any {
	raw, ok := results["data"].(*sources.TibberDataset)
	if !ok {
		return map[string]any{}
	}
	data := energyPrices(raw, cfg, time.Now())
	out := map[string]any{"Data": data, "EnergyOnly": cfg.EnergyOnly}

	// Prices hold for a whole hour, so the curve is a staircase:
	//	M0,y0 H40 V y1 H80 V y2 …   plus the cheapest window as a band
	//	and a line for now, on the same x scale.
	now := time.Now().UTC()
	if len(data.Prices) >= 2 {
		low, high := data.Prices[0].Total, data.Prices[0].Total
		for _, p := range data.Prices {
			low, high = min(low, p.Total), max(high, p.Total)
		}
		span := max(high-low, 0.01)
		step := float64(trendWidth) / float64(len(data.Prices))
		y := func(v float64) float64 { return trendHeight - (v-low)/span*(trendHeight-2*chartMargin) - chartMargin }
		path := fmt.Sprintf("M0,%.1f", y(data.Prices[0].Total))
		for i, p := range data.Prices {
			if i > 0 {
				path += fmt.Sprintf(" V%.1f", y(p.Total))
			}
			path += fmt.Sprintf(" H%.1f", float64(i+1)*step)
		}
		first := data.Prices[0].At
		x := func(t time.Time) float64 { return t.Sub(first).Hours() * step }
		out["Path"], out["W"], out["H"], out["Low"], out["High"] = path, trendWidth, trendHeight, low, high
		if nx := x(now); nx > 0 && nx < float64(trendWidth) {
			out["NowX"] = nx
		}
		if start, _, ok := metrics.CheapWindow(data.Prices, now, cfg.CheapHours); ok {
			out["CheapX"], out["CheapW"] = x(start), float64(cfg.CheapHours)*step
		}
	}
	if start, avg, ok := metrics.CheapWindow(data.Prices, now, cfg.CheapHours); ok {
		out["CheapStart"], out["CheapAvg"], out["CheapHours"] = start, avg, cfg.CheapHours
	}

	cost, kwh := 0.0, 0.0
	days := data.Days
	if len(days) > energyDaysCost {
		days = days[len(days)-energyDaysCost:]
	}
	for _, d := range days {
		cost, kwh = cost+d.Cost, kwh+d.KWh
	}
	out["Cost"], out["KWh"], out["Days"] = cost, kwh, len(days)

	if hass, ok := results[peerHass].(*sources.HassDataset); ok && cfg.PowerEntity != "" {
		if e, found := hass.Find(cfg.PowerEntity); found {
			out["Power"], out["PowerUnit"] = e.State, e.Unit
		}
	}
	return out
}

func init() {
	Tile[MediaConfig]{Key: "mediaserver", Detail: mediaDetail, Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceMediaServer, RefreshS: integrationTTL,
		Fields:  []Field{{Key: "show_users", Input: InputCheck, Default: true}},
		Decode:  func(r Raw) MediaConfig { return MediaConfig{Users: r.Bool("show_users")} },
		Queries: ownData[MediaConfig], View: mediaView}.add()

	Tile[ArrConfig]{Key: "arr_upcoming", Detail: dataDetail(arrDetail), Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceArr, RefreshS: integrationTTL,
		Fields:  []Field{{Key: "days", Input: InputNumber, Default: arrDays, Min: "1", Max: "30"}},
		Decode:  func(r Raw) ArrConfig { return ArrConfig{Days: r.Int("days")} },
		Queries: ownData[ArrConfig], View: dataView(arrView)}.add()

	Tile[GrocyConfig]{Key: "grocy", Detail: dataDetail(grocyDetail), Category: CategoryInsight, Topic: TopicHome, Service: enums.ServiceGrocy, RefreshS: integrationTTL,
		Fields: []Field{{Key: "show_stock", Input: InputCheck, Default: true}, {Key: "show_shopping", Input: InputCheck, Default: true},
			{Key: "show_chores", Input: InputCheck, Default: true}, {Key: "days", Input: InputNumber, Default: 0, Min: "0", Max: "60"}},
		Decode: decodeGrocy, Queries: ownData[GrocyConfig], View: dataView(grocyView)}.add()

	Tile[DWDConfig]{Key: "dwd", Detail: dataDetail(dwdDetail), Category: CategoryInsight, Topic: TopicHome, Service: enums.ServiceDWD, RefreshS: integrationTTL,
		Fields: []Field{sel("min_level", "minor", "minor", "moderate", "severe", "extreme")},
		Decode: decodeDWD, Queries: ownData[DWDConfig], View: dataView(dwdView)}.add()

	Tile[TailscaleConfig]{Key: "tailscale", Detail: dataDetail(tailscaleDetail), Category: CategoryInsight, Topic: TopicNetwork, Service: enums.ServiceTailscale, RefreshS: integrationTTL,
		Fields: []Field{{Key: "only_problems", Input: InputCheck}, {Key: "hide_after", Input: InputNumber, Default: 0, Min: "0", Max: "3650"},
			{Key: "tags", Input: InputList}},
		Renames: []rename{{from: "only_trouble", to: "only_problems"}},
		Decode:  decodeTailscale, Queries: ownData[TailscaleConfig], View: dataView(tailscaleView)}.add()

	Tile[GitHubConfig]{Key: "github", Detail: dataDetail(githubDetail), Category: CategoryInsight, Topic: TopicDev, Service: enums.ServiceGitHub, RefreshS: integrationTTL,
		Fields:  []Field{{Key: "filter", Input: InputList}, {Key: "only_problems", Input: InputCheck}},
		Renames: []rename{{from: "only_red", to: "only_problems"}},
		Decode:  decodeGitHub, Queries: ownData[GitHubConfig], View: dataView(githubView)}.add()

	Tile[EnergyConfig]{Key: "energy", Detail: energyDetail, Category: CategoryInsight, Topic: TopicHome, Service: enums.ServiceTibber, RefreshS: integrationTTL,
		Fields: []Field{{Key: "power_entity", Input: InputText}, {Key: "cheap_hours", Input: InputNumber, Default: metrics.CheapHours, Min: "1", Max: "12"},
			{Key: "tomorrow", Input: InputCheck, Default: true}, sel("price", "total", "total", "energy")},
		Decode: decodeEnergy, Queries: energyQueries, View: energyView}.add()
}
