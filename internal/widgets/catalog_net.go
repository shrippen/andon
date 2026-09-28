package widgets

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// ── dns_filter ──

// DNSConfig is the Pi-hole and AdGuard widgets' config.
type DNSConfig struct{ Clients, Domains bool }

func decodeDNS(raw map[string]any) any {
	return DNSConfig{Clients: asBool(raw["top_clients"]), Domains: asBool(raw["top_domains"])}
}

// dnsTop is how many clients or domains a DNS tile lists.
const dnsTop = 5

func dnsFilterView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(DNSConfig)
	data, ok := results["data"].(*sources.DNSFilterDataset)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{"Percent": data.Percent, "W": pctOf(data.Percent, pctFull), "Queries": data.Queries,
		"Blocked": data.Blocked, "Enabled": data.Enabled}
	if cfg.Clients {
		out["Clients"] = firstN(data.TopClients, dnsTop)
	}
	if cfg.Domains {
		out["Domains"] = firstN(data.TopBlocked, dnsTop)
	}
	return out
}

// ── vpn ──

// VPNConfig is the "vpn" widget's config.
type VPNConfig struct{ Country string }

func decodeVPN(raw map[string]any) any {
	return VPNConfig{Country: strings.TrimSpace(asString(raw["expected_country"]))}
}

func vpnView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(VPNConfig)
	raw, ok := results["data"].(*sources.GluetunDataset)
	if !ok {
		return map[string]any{}
	}
	data := raw
	if cfg.Country != "" {
		copied := *raw
		copied.ExpectedCountry = cfg.Country
		data = &copied
	}
	up := data.Status == "running"
	leak := data.ExitIP != "" && data.ExitIP == data.OwnIP
	wrongCountry := data.ExpectedCountry != "" && !strings.EqualFold(data.ExpectedCountry, data.Country)
	state := "ok"
	switch {
	case !up || leak:
		state = "fail"
	case wrongCountry:
		state = "warn"
	}
	return map[string]any{"Data": data, "Up": up, "Leak": leak, "WrongCountry": wrongCountry, "State": state}
}

// ── gateway ──

// GatewayConfig is the "gateway" widget's config.
type GatewayConfig struct {
	HideMeasures bool // no loss and latency per link
	DeviceList   bool // name the devices, not just count them
}

func decodeGateway(raw map[string]any) any {
	return GatewayConfig{HideMeasures: asBool(raw["hide_measures"]), DeviceList: asBool(raw["device_list"])}
}

// gatewayNames caps the devices a gateway tile names.
const gatewayNames = 30

func gatewayView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(GatewayConfig)
	data, ok := results["data"].(*sources.GatewayDataset)
	if !ok {
		return map[string]any{}
	}
	online, pending := 0, 0
	var offline []string
	for _, dev := range data.Devices {
		if dev.Online {
			online++
		} else if len(offline) < listShown {
			offline = append(offline, dev.Name)
		}
		if dev.Update {
			pending++
		}
	}
	out := map[string]any{"Links": data.Gateways, "Online": online, "Devices": len(data.Devices), "Offline": offline,
		"Updates": data.Updates + pending, "Kind": data.Kind, "Version": data.Version, "Model": data.Model, "Clients": data.Clients,
		// OpenWrt reports links without loss and delay, and no updates.
		"Measured": data.Kind != "openwrt", "ShowMeasures": data.Kind != "openwrt" && !cfg.HideMeasures}
	if cfg.DeviceList {
		var names []string
		for _, dev := range data.Devices {
			names = append(names, dev.Name)
		}
		names = append(names, data.ClientNames...)
		out["Names"] = strings.Join(firstN(names, gatewayNames), ", ")
		out["NamesMore"] = max(len(names)-gatewayNames, 0)
	}
	return out
}

// ── expiry ──

const peerDomains = "domains"

func expiryBar(label string, left int, err string) HBar {
	if err != "" {
		return HBar{Label: label, Value: err, Tier: "red"}
	}
	tier := ""
	switch {
	case left < expiryHigh:
		tier = "red"
	case left < expiryWarn:
		tier = "yellow"
	}
	return HBar{Label: label, Value: strconv.Itoa(left), W: max(pctOf(float64(left), expiryHorizon), 2), Tier: tier}
}

// expiryView lists certificates and domains soonest first:
//
//	shop.example 12 d (red) · nas.lan 40 d · example.org (domain) 200 d
//
// ExpiryConfig is the "expiry" widget's config.
type ExpiryConfig struct {
	MaxDays int    // hide what runs out later, 0 = show all
	Kinds   string // "", "certs" or "domains"
}

func decodeExpiry(raw map[string]any) any {
	kinds, _ := raw["kinds"].(string)
	if kinds == "both" {
		kinds = ""
	}
	return ExpiryConfig{MaxDays: clampInt(asInt(raw["max_days"], 0), 0, 3650), Kinds: kinds}
}

func expiryView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(ExpiryConfig)
	today := todayOf(ctx)
	daysTo := func(t time.Time) int { return int(t.Sub(today).Hours() / hoursPerDay) }

	type item struct {
		bar  HBar
		left int
	}
	var items []item
	keep := func(left int, failed string) bool { return cfg.MaxDays == 0 || left <= cfg.MaxDays || failed != "" }
	if certs, ok := results["data"].(*sources.CertDataset); ok && cfg.Kinds != "domains" {
		for _, c := range certs.Certs {
			if left := daysTo(c.NotAfter); keep(left, c.Error) {
				items = append(items, item{expiryBar(c.Host, left, c.Error), left})
			}
		}
	}
	if doms, ok := results[peerDomains].(*sources.DomainsDataset); ok && cfg.Kinds != "certs" {
		for _, dm := range doms.Domains {
			if left := daysTo(dm.Expires); keep(left, dm.Error) {
				items = append(items, item{expiryBar(dm.Name, left, dm.Error), left})
			}
		}
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].left < items[b].left })

	var bars []HBar
	for _, it := range items {
		if len(bars) < barsShown {
			bars = append(bars, it.bar)
		}
	}
	return map[string]any{"Bars": bars, "Total": len(items)}
}

// ── speed_history ──

// SpeedHistoryConfig is the "speed_history" widget's config.
type SpeedHistoryConfig struct{ Days int }

func decodeSpeedHistory(raw map[string]any) any {
	return SpeedHistoryConfig{Days: clampInt(asInt(raw["days"], speedDays), 2, 90)}
}

func speedHistoryView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	days := speedDays
	if cfg, ok := cfgAny.(SpeedHistoryConfig); ok && cfg.Days > 0 {
		days = cfg.Days
	}
	h, _ := results[HistorySlot].(*metrics.History)
	data, _ := results["data"].(*sources.SpeedtestDataset)
	if h == nil {
		return map[string]any{}
	}
	points := h.SeriesOf(metrics.SampleKey("speedtest", "down"))
	if len(points) > days {
		points = points[len(points)-days:]
	}
	expect := 0.0
	if data != nil {
		expect = data.ExpectDown
	}
	top := expect
	for _, p := range points {
		top = max(top, p.Value)
	}
	var bars []LoadBar
	slow := 0
	for _, p := range points {
		tier := ""
		if expect > 0 && p.Value < expect*speedWarn {
			tier = "yellow"
			slow++
		}
		bars = append(bars, LoadBar{H: max(pctOf(p.Value, top), 2), Tier: tier, Title: p.Day.Format(time.DateOnly)})
	}
	out := map[string]any{"Bars": bars, "Slow": slow, "Expect": expect, "ExpectPct": pctOf(expect, top)}
	if data != nil {
		out["Data"] = data
	}
	return out
}

// ── sabnzbd ──

// SabConfig is the "sabnzbd" widget's config.
type SabConfig struct{ Queue int }

func decodeSab(raw map[string]any) any {
	return SabConfig{Queue: clampInt(asInt(raw["queue"], 0), 0, 20)}
}

func sabnzbdView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(SabConfig)
	data, ok := results["data"].(*sources.SabnzbdDataset)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{"Data": data, "SpeedMB": data.SpeedKB / 1024, "Failures": firstN(data.Failures, listShown),
		"Queue": firstN(data.Queue, cfg.Queue)}
}

// ── paperless_inbox ──

// PaperlessConfig is the "paperless_inbox" widget's config.
type PaperlessConfig struct {
	Newest int    // latest documents listed
	Tag    string // count this tag instead of the inbox (lower case)
}

func decodePaperless(raw map[string]any) any {
	return PaperlessConfig{Newest: clampInt(asInt(raw["newest_docs"], 0), 0, 10), Tag: strings.ToLower(strings.TrimSpace(asString(raw["tag"])))}
}

func paperlessInboxView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(PaperlessConfig)
	data, ok := results["data"].(*sources.PaperlessDataset)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{"Data": data, "Count": data.Inbox, "Newest": firstN(data.Newest, cfg.Newest)}
	if cfg.Tag != "" {
		out["Tag"], out["Count"] = cfg.Tag, data.TagCounts[cfg.Tag]
		out["TagURL"] = strings.TrimRight(data.URL, "/") + "/documents?query=" + url.QueryEscape("tag:"+cfg.Tag)
		return out
	}
	if added, ok := metrics.ParseDay(data.OldestAdded); ok {
		out["OldestDays"] = int(todayOf(ctx).Sub(added).Hours() / hoursPerDay)
	}
	return out
}

// ── mail_invoices ──

// ForwardedSlot carries the UIDs of mails already sent to Paperless
// (map[uint32]bool) for the mail invoices tile.
const ForwardedSlot = "forwarded"

// MailConfig is the "mail_invoices" widget's config.
type MailConfig struct {
	OnlyOpen bool // leave out mails already sent to Paperless
	Limit    int
}

func decodeMail(raw map[string]any) any {
	return MailConfig{OnlyOpen: asBool(raw["only_open"]), Limit: clampInt(asInt(raw["limit"], listShown), 1, 30)}
}

func mailInvoicesView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(MailConfig)
	if !ok {
		cfg = decodeMail(nil).(MailConfig)
	}
	data, ok := results["data"].(*sources.MailDataset)
	if !ok {
		return map[string]any{}
	}
	sent, _ := results[ForwardedSlot].(map[uint32]bool)
	var list []sources.MailInvoice
	sum := 0.0
	for _, inv := range data.Invoices {
		if cfg.OnlyOpen && sent[inv.UID] {
			continue
		}
		sum += inv.Amount
		list = append(list, inv)
	}
	shown := append([]sources.MailInvoice(nil), list...)
	sort.Slice(shown, func(a, b int) bool { return shown[a].Date.After(shown[b].Date) })
	if len(shown) > cfg.Limit {
		shown = shown[:cfg.Limit]
	}
	return map[string]any{"Count": len(list), "Sum": sum, "Shown": shown, "Scanned": data.Scanned, "Mailbox": data.Mailbox}
}

// ── freshrss_feeds ──

// FreshRSSConfig is the "freshrss_feeds" widget's config.
type FreshRSSConfig struct {
	Only       []string // feed or category name parts (lower case), empty = all
	OnlyUnread bool
}

func decodeFreshRSS(raw map[string]any) any {
	return FreshRSSConfig{Only: lowerList(raw["filter"]), OnlyUnread: boolOr(raw["only_unread"], true)}
}

func freshrssView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(FreshRSSConfig)
	if !ok {
		cfg.OnlyUnread = true
	}
	data, ok := results["data"].(*sources.FreshRSSDataset)
	if !ok {
		return map[string]any{}
	}
	var feeds []sources.Feed
	unread := 0
	for _, f := range data.Feeds {
		if matchesAny(f.Title, cfg.Only) || matchesAny(f.Category, cfg.Only) {
			feeds = append(feeds, f)
			unread += f.Unread
		}
	}
	sort.Slice(feeds, func(a, b int) bool { return feeds[a].Unread > feeds[b].Unread })
	top := 1
	if len(feeds) > 0 {
		top = max(feeds[0].Unread, 1)
	}
	var bars []HBar
	for _, f := range feeds {
		if (f.Unread == 0 && cfg.OnlyUnread) || len(bars) >= barsShown {
			break
		}
		bars = append(bars, HBar{Label: f.Title, Value: strconv.Itoa(f.Unread), W: pctOf(float64(f.Unread), float64(top))})
	}
	if len(cfg.Only) == 0 {
		unread = data.Unread
	}
	return map[string]any{"Unread": unread, "Feeds": len(feeds), "Bars": bars}
}

// decodeEmptyConfig is for tiles without settings.
func decodeEmptyConfig(map[string]any) any { return struct{}{} }
