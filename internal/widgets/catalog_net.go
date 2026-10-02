package widgets

import (
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// ── dns_filter ──

// DNSConfig is the Pi-hole and AdGuard widgets' config.
type DNSConfig struct{ Clients, Domains bool }

func decodeDNS(r Raw) DNSConfig {
	return DNSConfig{Clients: r.Bool("top_clients"), Domains: r.Bool("top_domains")}
}

// dnsTop is how many clients or domains a DNS tile lists.
const dnsTop = 5

func init() {
	for key, service := range map[string]enums.ServiceType{"pihole": enums.ServicePihole, "adguard": enums.ServiceAdGuard} {
		Tile[DNSConfig]{Key: key, Detail: dataDetail(dnsDetail), Category: CategoryInsight, Topic: TopicNetwork, Service: service, RefreshS: 5 * 60,
			Fields: []Field{{Key: "top_clients", Input: InputCheck}, {Key: "top_domains", Input: InputCheck}},
			Decode: decodeDNS, Queries: ownData[DNSConfig], View: dataView(dnsFilterView)}.add()
	}
}

func dnsFilterView(cfg DNSConfig, data *sources.DNSFilterDataset, _ ViewCtx) map[string]any {
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

func init() {
	Tile[VPNConfig]{Key: "vpn", Detail: dataDetail(vpnDetail), Category: CategoryInsight, Topic: TopicNetwork, Service: enums.ServiceGluetun, RefreshS: 5 * 60,
		Fields: []Field{{Key: "expected_country", Input: InputText}},
		Decode: func(r Raw) VPNConfig {
			return VPNConfig{Country: strings.TrimSpace(r.String("expected_country"))}
		},
		Queries: ownData[VPNConfig], View: dataView(vpnView)}.add()
}

func vpnView(cfg VPNConfig, raw *sources.GluetunDataset, _ ViewCtx) map[string]any {
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

func init() {
	Tile[GatewayConfig]{Key: "gateway", Detail: dataDetail(gatewayDetail), Category: CategoryInsight, Topic: TopicNetwork, Service: enums.ServiceGateway, RefreshS: 5 * 60,
		Fields: []Field{{Key: "hide_measures", Input: InputCheck}, {Key: "device_list", Input: InputCheck}},
		Decode: func(r Raw) GatewayConfig {
			return GatewayConfig{HideMeasures: r.Bool("hide_measures"), DeviceList: r.Bool("device_list")}
		},
		Queries: ownData[GatewayConfig], View: dataView(gatewayView)}.add()
}

// gatewayNames caps the devices a gateway tile names.
const gatewayNames = 30

func gatewayView(cfg GatewayConfig, data *sources.GatewayDataset, _ ViewCtx) map[string]any {
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

// expiryUnknown is the value of a bar whose end is not known; such a
// bar sorts last (expiryNever days left).
const (
	expiryUnknown = "expiry.unknown"
	expiryNever   = math.MaxInt32
)

// expiryBar: days left as a bar; an error or an unknown end as text
// without a bar (W 0).
func expiryBar(label string, left int, err string) HBar {
	if err != "" {
		return HBar{Label: label, Value: err, Tier: "red"}
	}
	if left == expiryNever {
		return HBar{Label: label, Value: expiryUnknown}
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

// expiryBoth shows certificates and domains.
const expiryBoth = "both"

func decodeExpiry(r Raw) ExpiryConfig {
	kinds := r.Pick("kinds")
	if kinds == expiryBoth {
		kinds = ""
	}
	return ExpiryConfig{MaxDays: r.Int("max_days"), Kinds: kinds}
}

func init() {
	Tile[ExpiryConfig]{Key: "expiry", Detail: expiryDetail, Category: CategoryInsight, Topic: TopicSecurity, Service: enums.ServiceCerts, RefreshS: 60 * 60,
		Fields: []Field{{Key: "max_days", Input: InputNumber, Min: "0", Max: "3650"}, sel("kinds", expiryBoth, expiryBoth, "certs", "domains")},
		Decode: decodeExpiry, View: expiryView,
		Queries: func(ExpiryConfig) []Query {
			return append(dataQuery(nil), Query{Name: peerDomains, Source: "data", Conn: ConnPeer, Service: enums.ServiceDomains})
		},
		Calm: func(v map[string]any) bool { return v["Total"] == 0 }}.add()
}

func expiryView(cfg ExpiryConfig, results map[string]any, ctx ViewCtx) map[string]any {
	today := todayOf(ctx)
	daysTo := func(t time.Time) int {
		if t.IsZero() {
			return expiryNever
		}
		return int(t.Sub(today).Hours() / hoursPerDay)
	}

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

func init() {
	Tile[SpeedHistoryConfig]{Key: "speed_history", Detail: speedHistoryDetail, Category: CategoryInsight, Topic: TopicNetwork, Service: enums.ServiceSpeedtest,
		RefreshS: 60 * 60, Extra: ExtraHistory,
		Fields:  []Field{{Key: "days", Input: InputNumber, Default: speedDays, Min: "2", Max: "90"}},
		Decode:  func(r Raw) SpeedHistoryConfig { return SpeedHistoryConfig{Days: r.Int("days")} },
		Queries: ownData[SpeedHistoryConfig], View: speedHistoryView}.add()
}

func speedHistoryView(cfg SpeedHistoryConfig, results map[string]any, _ ViewCtx) map[string]any {
	days := speedDays
	if cfg.Days > 0 {
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

func init() {
	Tile[SabConfig]{Key: "sabnzbd", Detail: dataDetail(sabDetail), Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceSabnzbd, RefreshS: 5 * 60,
		Fields:  []Field{{Key: "queue", Input: InputNumber, Default: 0, Min: "0", Max: "20"}},
		Decode:  func(r Raw) SabConfig { return SabConfig{Queue: r.Int("queue")} },
		Queries: ownData[SabConfig], View: dataView(sabnzbdView)}.add()
}

func sabnzbdView(cfg SabConfig, data *sources.SabnzbdDataset, _ ViewCtx) map[string]any {
	return map[string]any{"Data": data, "SpeedMB": data.SpeedKB / 1024, "Failures": firstN(data.Failures, listShown),
		"Queue": firstN(data.Queue, cfg.Queue)}
}

// ── paperless_inbox ──

// PaperlessConfig is the "paperless_inbox" widget's config.
type PaperlessConfig struct {
	Newest int    // latest documents listed
	Tag    string // count this tag instead of the inbox (lower case)
}

func init() {
	Tile[PaperlessConfig]{Key: "paperless_inbox", Detail: dataDetail(paperlessDetail), Category: CategoryInsight, Topic: TopicWork, Service: enums.ServicePaperless, RefreshS: 30 * 60,
		Fields: []Field{{Key: "newest_docs", Input: InputNumber, Default: 0, Min: "0", Max: "10"}, {Key: "tag", Input: InputText}},
		Decode: func(r Raw) PaperlessConfig {
			return PaperlessConfig{Newest: r.Int("newest_docs"), Tag: strings.ToLower(strings.TrimSpace(r.String("tag")))}
		},
		Queries: ownData[PaperlessConfig], View: dataView(paperlessInboxView),
		// Count is the inbox, or the tag's documents when a tag is set.
		Calm: func(v map[string]any) bool {
			_, ok := v["Data"].(*sources.PaperlessDataset)
			return ok && v["Count"] == 0
		}}.add()
}

func paperlessInboxView(cfg PaperlessConfig, data *sources.PaperlessDataset, ctx ViewCtx) map[string]any {
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

func init() {
	Tile[MailConfig]{Key: "mail_invoices", Detail: mailInvoicesDetail, Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceMail, RefreshS: 60 * 60,
		Extra:   ExtraForwarded,
		Fields:  []Field{{Key: "only_open", Input: InputCheck}, {Key: "limit", Input: InputNumber, Default: listShown, Min: "1", Max: "30"}},
		Decode:  func(r Raw) MailConfig { return MailConfig{OnlyOpen: r.Bool("only_open"), Limit: r.Int("limit")} },
		Queries: ownData[MailConfig], View: mailInvoicesView}.add()
}

func mailInvoicesView(cfg MailConfig, results map[string]any, _ ViewCtx) map[string]any {
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

func init() {
	Tile[FreshRSSConfig]{Key: "freshrss_feeds", Detail: dataDetail(freshrssDetail), Category: CategoryInsight, Topic: TopicMedia, Service: enums.ServiceFreshRSS, RefreshS: 30 * 60,
		Fields: []Field{{Key: "filter", Input: InputList}, {Key: "only_unread", Input: InputCheck, Default: true}},
		Decode: func(r Raw) FreshRSSConfig {
			return FreshRSSConfig{Only: r.Lower("filter"), OnlyUnread: r.Bool("only_unread")}
		},
		Queries: ownData[FreshRSSConfig], View: dataView(freshrssView)}.add()
}

func freshrssView(cfg FreshRSSConfig, data *sources.FreshRSSDataset, _ ViewCtx) map[string]any {
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
