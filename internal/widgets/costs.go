package widgets

// "homelab_cost": what the homelab costs per month (power, hardware
// write-off, domains, hosting), the power split by Proxmox guests, the
// share used for customers, and a cloud comparison. Settings: space
// settings → Homelab costs.

import (
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// costPeers are the services the bill is built from.
var costPeers = []enums.ServiceType{
	enums.ServiceHomeAssistant, enums.ServiceTibber, enums.ServiceSnipeIT, enums.ServiceSure, enums.ServiceDomains,
	enums.ServiceKimai, enums.ServiceKomodo, enums.ServiceGitea, enums.ServiceGitHub, enums.ServiceProxmox,
}

// CostConfig is the "homelab_cost" widget's config.
type CostConfig struct {
	Yearly     bool // amounts per year
	PowerSplit bool // the power cost per Proxmox guest
}

func decodeCost(raw map[string]any) any {
	return CostConfig{Yearly: raw["period"] == "year", PowerSplit: boolOr(raw["power_split"], true)}
}

// scaleBill turns the monthly bill into the tile's period.
func scaleBill(b metrics.HomelabBill, f float64) metrics.HomelabBill {
	items := make([]metrics.CostItem, len(b.Items))
	for i, it := range b.Items {
		it.Monthly *= f
		items[i] = it
	}
	b.Items, b.Total, b.Cloud = items, b.Total*f, b.Cloud*f
	return b
}

func homelabCostView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, ok := cfgAny.(CostConfig)
	if !ok {
		cfg = CostConfig{PowerSplit: true}
	}
	scale := 1.0
	if cfg.Yearly {
		scale = monthsPerYearF
	}
	ds := peerDatasets(results, costPeers)
	s := metrics.HomelabSettingsOf(ctx.Settings)
	in := metrics.CostInputs{}
	in.Hass, _ = ds[string(enums.ServiceHomeAssistant)].(*sources.HassDataset)
	in.Tibber, _ = ds[string(enums.ServiceTibber)].(*sources.TibberDataset)
	in.Snipe, _ = ds[string(enums.ServiceSnipeIT)].(*sources.SnipeDataset)
	in.Sure, _ = ds[string(enums.ServiceSure)].(*sources.SureDataset)
	in.Domains, _ = ds[string(enums.ServiceDomains)].(*sources.DomainsDataset)
	bill := metrics.HomelabCost(in, s, time.Now().UTC())

	out := map[string]any{"Bill": scaleBill(bill, scale), "Settings": s, "Yearly": cfg.Yearly}
	kimai, _ := ds[string(enums.ServiceKimai)].(*sources.KimaiDataset)
	if share, business, all := metrics.BusinessShare(kimai, metrics.WorkNames(ds)); all > 0 && kimai != nil {
		out["Share"], out["SharePct"], out["ShareOf"], out["ShareAll"] = share, share*100, business, all
		out["BusinessYearly"] = bill.Total * share * monthsPerYearF
	}
	if w, ok := metrics.PowerWatts(in.Hass, s.PowerEntity); ok && cfg.PowerSplit {
		monthly := metrics.MonthlyPowerCost(w, metrics.PowerPrice(in.Tibber, s.PowerPrice))
		proxmox, _ := ds[string(enums.ServiceProxmox)].(*sources.ProxmoxDataset)
		services := metrics.PowerPerService(proxmox, monthly*scale)
		out["Watts"], out["PowerMonthly"], out["Services"] = w, monthly*scale, services
	}
	return out
}

// monthsPerYearF turns monthly into yearly amounts.
const monthsPerYearF = 12.0

// StorySlot names the week's story among a widget's results.
const StorySlot = "story"

// storyParts maps a story line to the checkbox that hides it.
var storyParts = map[string]string{"hours": "show_hours", "invoiced": "show_money", "paid": "show_money",
	"storage": "show_storage", "power": "show_power", "hints": "show_hints"}

// StoryConfig is the "week_story" widget's config.
type StoryConfig struct {
	Hide     map[string]bool // line keys left out
	Calendar bool            // since Monday instead of the last 7 days
}

func decodeStory(raw map[string]any) any {
	cfg := StoryConfig{Hide: map[string]bool{}, Calendar: raw["period"] == "calendar"}
	for line, box := range storyParts {
		if !boolOr(raw[box], true) {
			cfg.Hide[line] = true
		}
	}
	return cfg
}

func storyView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(StoryConfig)
	lines, _ := results[StorySlot].([]metrics.StoryLine)
	var shown []metrics.StoryLine
	for _, l := range lines {
		if !cfg.Hide[l.Key] {
			shown = append(shown, l)
		}
	}
	return map[string]any{"Lines": shown, "Calendar": cfg.Calendar}
}

func init() {
	Register(WidgetType{Key: "week_story", Decode: decodeStory, Category: CategoryInsight,
		RefreshS: 3600, View: storyView, Extra: ExtraStory})
	Register(WidgetType{Key: "homelab_cost", Decode: decodeCost, Category: CategoryInsight,
		RefreshS: 3600, View: homelabCostView, Queries: func(any) []Query { return peersOf(costPeers) }})
}
