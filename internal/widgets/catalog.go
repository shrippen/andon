package widgets

// The data tiles of the catalog (ROADMAP 9.2) on one service each:
//
//	kimai_week      hours per weekday against a daily target
//	kimai_split     this week's hours per day, stacked by customer
//	unbilled_age    billable, unexported Kimai time stacked by age
//	disks           Scrutiny: every disk's state, temperature, hours
//	komodo_stacks   Komodo: servers and stacks, pending image updates
//	truenas_pools   TrueNAS: pool fill and state, alerts, app updates
//	dns_filter      Pi-hole / AdGuard: blocked share today
//	vpn             Gluetun: tunnel state, exit country, leak
//	gateway         OPNsense, pfSense, UniFi: WAN links, devices, updates
//	expiry          certificates (and domains): days left as bars
//	speed_history   Speedtest: the last days' download against the contract
//	sabnzbd         SABnzbd: speed, queue, free space, failures
//	paperless_inbox Paperless: inbox size and its oldest document
//	mail_invoices   invoices found in the mailbox
//	freshrss_feeds  FreshRSS: unread per feed
//	linkwarden      Linkwarden: links per collection
//	gitea_reviews   Gitea: reviews waiting, assigned issues
//	dawarich_day    Dawarich: today's places on an hour bar
//	authentik_logins authentik: logins and failures, latest logins
//	vaultwarden_2fa Vaultwarden: accounts with and without two-factor
//	kintsugi        Kintsugi: open acquisition suggestions, take-up, gaps

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	listShown     = 5
	barsShown     = 6
	weekDays      = 7
	workDays      = 5
	defaultWeekH  = 40
	expiryHorizon = 90 // days a full expiry bar stands for
	expiryWarn    = 30
	expiryHigh    = 14
	tempWarn      = 45
	tempHigh      = 55
	hoursPerDay   = 24
	speedDays     = 7
	speedWarn     = 0.8 // share of the contract below which a day is slow
	splitShown    = 4   // customers with their own colour, the rest is "other"
)

// splitTiers colours customers in the kimai_split stack.
var splitTiers = []string{"blue", "purple", "aqua", "orange", "grey"}

// HBar is one labelled horizontal bar (feeds, collections, expiry).
type HBar struct {
	Label string
	Value string
	W     int // width in percent
	Tier  string
}

// Seg is one part of a stacked column, height in percent of the column.
type Seg struct {
	H    int
	Tier string
}

// DayCol is one day of a weekday chart.
type DayCol struct {
	I     int // 0 = Monday
	H     int // total height in percent
	Hours string
	Tier  string
	Segs  []Seg
	Today bool // outlined
	Later bool // still to come: dimmed, never flagged
}

func pctOf(v, full float64) int {
	if full <= 0 {
		return 0
	}
	return int(min(max(v/full, 0), 1)*pctFull + 0.5)
}

// weekStart is Monday 00:00 of today's week.
func weekStart(today time.Time) time.Time {
	d := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func todayOf(ctx ViewCtx) time.Time {
	t, err := time.Parse(time.DateOnly, ctx.Today)
	if err != nil {
		return time.Now()
	}
	return t
}

// weekPick narrows which Kimai time counts.
type weekPick struct {
	Back      int  // weeks before this one
	Billable  bool // only billable time
	ByProject bool // group by project instead of customer
}

// weekMinutes sums a week's Kimai minutes per weekday, by customer (or
// project).
func weekMinutes(data *sources.KimaiDataset, today time.Time, pick weekPick) ([weekDays]int, [weekDays]map[int64]int) {
	var total [weekDays]int
	var byKey [weekDays]map[int64]int
	start := weekStart(today).AddDate(0, 0, -weekDays*pick.Back)
	for _, s := range data.Timesheets {
		if pick.Billable && !s.Billable {
			continue
		}
		day, ok := metrics.ParseDay(s.Begin)
		if !ok {
			continue
		}
		i := int(day.Sub(start).Hours() / hoursPerDay)
		if i < 0 || i >= weekDays {
			continue
		}
		total[i] += s.Minutes
		if byKey[i] == nil {
			byKey[i] = map[int64]int{}
		}
		key := s.CustomerID
		if pick.ByProject {
			key = s.ProjectID
		}
		byKey[i][key] += s.Minutes
	}
	return total, byKey
}

// ── kimai_week ──

// KimaiWeekConfig is the "kimai_week" widget's config.
type KimaiWeekConfig struct {
	WeekHours    float64
	Workdays     int  // 5 (Mo–Fr) or 6 (Mo–Sa)
	BillableOnly bool // leave internal time out
}

func decodeKimaiWeek(raw map[string]any) any {
	h := asFloat(raw["week_hours"])
	if h <= 0 {
		h = defaultWeekH
	}
	days := workDays
	if raw["workdays"] == "mo_sa" {
		days = workDays + 1
	}
	return KimaiWeekConfig{WeekHours: h, Workdays: days, BillableOnly: asBool(raw["billable_only"])}
}

// kimaiWeekView draws Mon–Sun against the daily target (week / workdays):
//
//	target 8 h, Mo 6.5 h Di 9 h  →  Mo yellow below the line, Di over it
func kimaiWeekView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(KimaiWeekConfig)
	data, ok := results["data"].(*sources.KimaiDataset)
	if !ok {
		return map[string]any{}
	}
	total, _ := weekMinutes(data, todayOf(ctx), weekPick{Billable: cfg.BillableOnly})
	days := cfg.Workdays
	if days <= 0 {
		days = workDays
	}

	target := cfg.WeekHours / float64(days) * minutesPerHour
	top := target
	sum := 0
	for _, m := range total {
		top = max(top, float64(m))
		sum += m
	}
	cols := make([]DayCol, weekDays)
	todayIdx := int(todayOf(ctx).Sub(weekStart(todayOf(ctx))).Hours() / hoursPerDay)
	for i, m := range total {
		tier := ""
		if i < days && i < todayIdx && float64(m) < target {
			tier = "yellow"
		}
		cols[i] = DayCol{I: i, H: max(pctOf(float64(m), top), 2), Hours: clockMinutes(m), Tier: tier,
			Today: i == todayIdx, Later: i > todayIdx}
	}
	left := int(cfg.WeekHours*minutesPerHour) - sum
	return map[string]any{"Days": cols, "TargetPct": pctOf(target, top), "Total": clockMinutes(sum),
		"Left": clockMinutes(max(left, -left)), "Over": left < 0, "WeekHours": cfg.WeekHours}
}

// ── kimai_split ──

// SplitLegend is one customer of the stacked week.
type SplitLegend struct {
	Name, Hours, Tier string
}

// kimaiSplitView stacks each day by customer; the busiest customers keep
// their own colour, the rest share "other".
// KimaiSplitConfig is the "kimai_split" widget's config.
type KimaiSplitConfig struct {
	LastWeek  bool
	ByProject bool
}

func decodeKimaiSplit(raw map[string]any) any {
	return KimaiSplitConfig{LastWeek: raw["week"] == "last", ByProject: raw["group"] == "project"}
}

func kimaiSplitView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(KimaiSplitConfig)
	data, ok := results["data"].(*sources.KimaiDataset)
	if !ok {
		return map[string]any{}
	}
	pick := weekPick{ByProject: cfg.ByProject}
	if cfg.LastWeek {
		pick.Back = 1
	}
	total, byCustomer := weekMinutes(data, todayOf(ctx), pick)
	names := metrics.KimaiCustomerNames(data)
	if cfg.ByProject {
		names = map[int64]string{}
		for _, p := range data.Projects {
			names[p.ID] = p.Name
		}
	}

	sums := map[int64]int{}
	top := 1
	for i := range weekDays {
		top = max(top, total[i])
		for c, m := range byCustomer[i] {
			sums[c] += m
		}
	}
	order := make([]int64, 0, len(sums))
	for c := range sums {
		order = append(order, c)
	}
	sort.Slice(order, func(a, b int) bool { return sums[order[a]] > sums[order[b]] })

	tierOf := map[int64]string{}
	other := len(splitTiers) - 1
	var legend []SplitLegend
	otherMin := 0
	for i, c := range order {
		if i < splitShown && i < other {
			tierOf[c] = splitTiers[i]
			name := names[c]
			if name == "" {
				name = "?"
			}
			legend = append(legend, SplitLegend{Name: name, Hours: clockMinutes(sums[c]), Tier: splitTiers[i]})
			continue
		}
		tierOf[c] = splitTiers[other]
		otherMin += sums[c]
	}
	if otherMin > 0 {
		legend = append(legend, SplitLegend{Name: "", Hours: clockMinutes(otherMin), Tier: splitTiers[other]})
	}

	cols := make([]DayCol, weekDays)
	for i := range weekDays {
		col := DayCol{I: i, H: pctOf(float64(total[i]), float64(top)), Hours: clockMinutes(total[i])}
		byTier := map[string]int{}
		for c, m := range byCustomer[i] {
			byTier[tierOf[c]] += m
		}
		for _, tier := range splitTiers {
			if m := byTier[tier]; m > 0 {
				col.Segs = append(col.Segs, Seg{H: pctOf(float64(m), float64(max(total[i], 1))), Tier: tier})
			}
		}
		cols[i] = col
	}
	return map[string]any{"Days": cols, "Legend": legend}
}

// ── unbilled_age ──

// UnbilledRow is one customer's unbilled money by age.
type UnbilledRow struct {
	Customer           string
	Fresh, Mid, Old    float64
	Total              float64
	FreshW, MidW, OldW int
}

func unbilledAgeView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, ok := cfgAny.(AgingConfig)
	if !ok {
		cfg = decodeAging([2]int{30, 60})(nil).(AgingConfig)
	}
	data, ok := results["data"].(*sources.KimaiDataset)
	if !ok {
		return map[string]any{}
	}
	internal := map[string]bool{}
	if cfg.HideInternal {
		for _, name := range internalCustomers(ctx.Settings) {
			internal[name] = true
		}
	}
	var rows []UnbilledRow
	var fresh, mid, old float64
	top := 0.0
	for _, r := range metrics.UnbilledAgingBy(data, todayOf(ctx), cfg.Mid, cfg.Old) {
		name := strings.ToLower(r.Customer)
		if internal[name] || slices.Contains(cfg.HideClients, name) {
			continue
		}
		row := UnbilledRow{Customer: r.Customer, Fresh: r.Fresh, Mid: r.Mid, Old: r.Old, Total: r.Fresh + r.Mid + r.Old}
		fresh, mid, old = fresh+r.Fresh, mid+r.Mid, old+r.Old
		top = max(top, row.Total)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].Total > rows[b].Total })
	if len(rows) > listShown {
		rows = rows[:listShown]
	}
	for i := range rows {
		rows[i].FreshW, rows[i].MidW, rows[i].OldW = pctOf(rows[i].Fresh, top), pctOf(rows[i].Mid, top), pctOf(rows[i].Old, top)
	}
	total := fresh + mid + old
	return map[string]any{"Total": total, "Rows": rows,
		"Bands": []AgingBand{
			{Key: "fresh", Tier: "green", Amount: fresh, Pct: pctOf(fresh, total), To: cfg.Mid},
			{Key: "mid", Tier: "yellow", Amount: mid, Pct: pctOf(mid, total), From: cfg.Mid + 1, To: cfg.Old},
			{Key: "old", Tier: "red", Amount: old, Pct: pctOf(old, total), Over: cfg.Old},
		}}
}

// internalCustomers are the space's customers whose time is never billed
// (settings billing.internal, comma separated), lower case.
func internalCustomers(settings map[string]any) []string {
	list, _ := settingsMap(settings, "billing")["internal"].(string)
	var out []string
	for _, name := range strings.Split(list, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ── disks ──

// DiskRow is one disk as drawn.
type DiskRow struct {
	Name, Model string
	OK          bool
	Temp        float64
	TempTier    string
	Years       float64 // power-on time
}

// DisksConfig is the "disks" widget's config.
type DisksConfig struct {
	TempWarn     float64 // °C from which a disk turns yellow; red 10 °C above
	OnlyProblems bool
}

func decodeDisks(raw map[string]any) any {
	warn := asFloat(raw["temp_warn"])
	if warn <= 0 {
		warn = tempWarn
	}
	return DisksConfig{TempWarn: warn, OnlyProblems: asBool(raw["only_problems"])}
}

func disksView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(DisksConfig)
	if !ok {
		cfg = DisksConfig{TempWarn: tempWarn}
	}
	data, ok := results["data"].(*sources.ScrutinyDataset)
	if !ok {
		return map[string]any{}
	}
	healthy := 0
	var rows []DiskRow
	for _, d := range data.Disks {
		row := DiskRow{Name: d.Name, Model: d.Model, OK: d.Status == sources.ScrutinyPassed, Temp: d.Temp,
			Years: float64(d.Hours) / hoursPerDay / 365}
		switch {
		case d.Temp >= cfg.TempWarn+tempHigh-tempWarn:
			row.TempTier = "red"
		case d.Temp >= cfg.TempWarn:
			row.TempTier = "yellow"
		}
		if row.OK {
			healthy++
		}
		if cfg.OnlyProblems && row.OK && row.TempTier == "" {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool { return !rows[a].OK && rows[b].OK })
	return map[string]any{"Healthy": healthy, "Total": len(data.Disks), "Rows": rows}
}

// ── komodo_stacks ──

func stackState(state string) string {
	switch strings.ToLower(state) {
	case "running", "healthy":
		return "ok"
	case "down", "unhealthy", "dead", "restarting":
		return "bad"
	default:
		return "mid"
	}
}

// KomodoConfig is the "komodo_stacks" widget's config.
type KomodoConfig struct {
	Only       []string // stack name parts (lower case), empty = all
	OnlyIssues bool     // only stacks not running or with updates
}

func decodeKomodo(raw map[string]any) any {
	return KomodoConfig{Only: lowerList(raw["filter"]), OnlyIssues: asBool(raw["only_issues"])}
}

func komodoView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(KomodoConfig)
	data, ok := results["data"].(*sources.KomodoDataset)
	if !ok {
		return map[string]any{}
	}
	var cells []StripCell
	var trouble []string
	updates, running, stacks := 0, 0, 0
	for _, s := range data.Stacks {
		if !matchesAny(s.Name, cfg.Only) {
			continue
		}
		stacks++
		state := stackState(s.State)
		if state == "ok" {
			running++
		} else if len(trouble) < listShown {
			trouble = append(trouble, s.Name+" · "+s.State)
		}
		if len(s.Updates) > 0 {
			updates++
		}
		if cfg.OnlyIssues && state == "ok" && len(s.Updates) == 0 {
			continue
		}
		cells = append(cells, StripCell{State: state, Title: s.Name + " · " + s.State})
	}
	return map[string]any{"Servers": data.ServersHealthy, "ServersTotal": data.ServersTotal, "Running": running,
		"Stacks": stacks, "Cells": cells, "Trouble": trouble, "Updates": updates, "Alerts": len(data.Alerts)}
}

// ── truenas_pools ──

// TrueNASConfig is the "truenas_pools" widget's config.
type TrueNASConfig struct {
	WarnPct  float64 // fill level from which a pool turns yellow; red 20 points above
	AppList  bool    // name the apps with updates
	Forecast bool    // "full in … days" per pool from the history
}

func decodeTrueNAS(raw map[string]any) any {
	warn := asFloat(raw["warn_pct"])
	if warn <= 0 || warn > pctFull {
		warn = loadWarn
	}
	return TrueNASConfig{WarnPct: warn, AppList: asBool(raw["app_updates"]), Forecast: asBool(raw["forecast"])}
}

// PoolBar is one pool: its bar and, with the forecast on, when it is full.
type PoolBar struct {
	HBar
	FullIn int // days, -1 = not filling or unknown
}

func truenasView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, ok := cfgAny.(TrueNASConfig)
	if !ok {
		cfg = TrueNASConfig{WarnPct: loadWarn}
	}
	data, ok := results["data"].(*sources.TrueNASDataset)
	if !ok {
		return map[string]any{}
	}
	fullIn := map[string]int{}
	if h, _ := results[HistorySlot].(*metrics.History); cfg.Forecast && h != nil {
		for _, f := range metrics.StorageForecasts(h, parseToday(ctx.Today)) {
			fullIn[f.Key] = f.FullIn
		}
	}
	var pools []PoolBar
	for _, p := range data.Pools {
		used := 0.0
		if p.Size > 0 {
			used = p.Allocated / p.Size
		}
		tier := ""
		switch {
		case !p.Healthy || used*pctFull >= cfg.WarnPct+loadHigh-loadWarn:
			tier = "red"
		case used*pctFull >= cfg.WarnPct:
			tier = "yellow"
		}
		bar := PoolBar{HBar: HBar{Label: p.Name + " · " + p.Status, W: pctOf(used, 1), Tier: tier}, FullIn: -1}
		if days, ok := fullIn["truenas.pool."+p.Name+".used"]; ok {
			bar.FullIn = days
		}
		pools = append(pools, bar)
	}
	updates := 0
	var apps []string
	for _, a := range data.Apps {
		if a.Update {
			updates++
			apps = append(apps, a.Name)
		}
	}
	out := map[string]any{"Pools": pools, "Alerts": len(data.Alerts), "Updates": updates, "Version": data.Version, "Forecast": cfg.Forecast}
	if cfg.AppList {
		out["Apps"] = strings.Join(apps, ", ")
	}
	return out
}

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
	out := map[string]any{"Data": data}
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
		y := parseToday(ctx.Today).AddDate(0, 0, -1)
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
	return AuthentikConfig{Day: raw["span"] == "24h", OnlyFailures: asBool(raw["only_failures"])}
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
