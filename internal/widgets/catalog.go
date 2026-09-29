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
	"slices"
	"sort"
	"strings"
	"time"

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
	// TargetPct is the day's own target line (Kimai work contract), 0 = none.
	TargetPct int
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

// KimaiWeekConfig is the "kimai_week" widget's config. Daily targets
// come from the Kimai work contract, never from here.
type KimaiWeekConfig struct {
	BillableOnly bool // leave internal time out
}

func decodeKimaiWeek(raw map[string]any) any {
	return KimaiWeekConfig{BillableOnly: asBool(raw["billable_only"])}
}

// kimaiWeekView draws Mon–Sun against each day's contract target:
//
//	Mo target 8 h, 6.5 h booked  →  yellow below its line
//	Sa target 0                  →  no line, never yellow
func kimaiWeekView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(KimaiWeekConfig)
	data, ok := results["data"].(*sources.KimaiDataset)
	if !ok {
		return map[string]any{}
	}
	today := todayOf(ctx)
	monday := weekStart(today)
	total, _ := weekMinutes(data, today, weekPick{Billable: cfg.BillableOnly})

	targets := make([]float64, weekDays)
	top := 0.0
	for i := range targets {
		targets[i] = float64(data.Contract.Minutes(monday.AddDate(0, 0, i)))
		top = max(top, targets[i])
	}
	sum := 0
	for _, m := range total {
		top = max(top, float64(m))
		sum += m
	}
	cols := make([]DayCol, weekDays)
	todayIdx := int(today.Sub(monday).Hours() / hoursPerDay)
	for i, m := range total {
		tier := ""
		if targets[i] > 0 && i < todayIdx && float64(m) < targets[i] {
			tier = "yellow"
		}
		cols[i] = DayCol{I: i, H: max(pctOf(float64(m), top), 2), Hours: clockMinutes(m), Tier: tier,
			Today: i == todayIdx, Later: i > todayIdx, TargetPct: pctOf(targets[i], top)}
	}
	week := data.Contract.WeekMinutes()
	left := week - sum
	return map[string]any{"Days": cols, "Total": clockMinutes(sum), "Contract": week > 0,
		"Left": clockMinutes(max(left, -left)), "Over": left < 0, "WeekHours": float64(week) / minutesPerHour}
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
	Stale       bool      // collector silent: values are old
	Seen        time.Time // last collector run
	Temp        float64
	TempTier    string
	Years       float64 // power-on time
}

// diskStaleDays: a disk not reported for longer shows grey, like the
// scrutiny.stale rule's default.
const diskStaleDays = 2

// DisksConfig is the "disks" widget's config.
type DisksConfig struct {
	TempWarn     float64 // °C from which a disk turns yellow; red 10 °C above
	OnlyProblems bool
}

func decodeDisks(raw map[string]any) any {
	warn := asFloat(raw["temp_warn"])
	if warn <= 0 || warn > pctFull {
		warn = tempWarn
	}
	return DisksConfig{TempWarn: warn, OnlyProblems: asBool(raw["only_problems"])}
}

func disksView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
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
	staleBefore := parseToday(ctx.Today).AddDate(0, 0, -diskStaleDays)
	for _, d := range data.Disks {
		row := DiskRow{Name: d.Name, Model: d.Model, OK: d.Status == sources.ScrutinyPassed, Temp: d.Temp,
			Years: float64(d.Hours) / hoursPerDay / 365, Seen: d.Seen}

		// Old values say nothing about the disk now: neither ok nor hot.
		if !d.Seen.IsZero() && d.Seen.Before(staleBefore) {
			row.Stale, row.OK = true, false
			rows = append(rows, row)
			continue
		}

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
	sort.SliceStable(rows, func(a, b int) bool { return diskRank(rows[a]) < diskRank(rows[b]) })
	return map[string]any{"Healthy": healthy, "Total": len(data.Disks), "Rows": rows}
}

// diskRank orders failed disks first, then stale ones, then healthy ones.
func diskRank(r DiskRow) int {
	switch {
	case r.Stale:
		return 1
	case r.OK:
		return 2
	}
	return 0
}

// ── komodo_stacks ──

// stackOff is a stack stopped on purpose (the connection's "stopped").
const stackOff = "off"

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
	return KomodoConfig{Only: lowerList(raw["filter"]), OnlyIssues: asBool(raw["only_problems"])}
}

func komodoView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := cfgAny.(KomodoConfig)
	data, ok := results["data"].(*sources.KomodoDataset)
	if !ok {
		return map[string]any{}
	}
	var cells []StripCell
	var trouble []string
	updates, running, stacks, resting := 0, 0, 0, 0
	stopped := metrics.KomodoStopped(ctx.Options)
	for _, s := range data.Stacks {
		if !matchesAny(s.Name, cfg.Only) {
			continue
		}
		stacks++
		state := stackState(s.State)

		// Stopped on purpose: grey, no trouble.
		if stopped[strings.ToLower(s.Name)] && state != "ok" {
			resting++
			if cfg.OnlyIssues {
				continue
			}
			cells = append(cells, StripCell{State: stackOff, Title: s.Name + " · " + s.State})
			continue
		}
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
		"Stacks": stacks, "Resting": resting, "Cells": cells, "Trouble": trouble, "Updates": updates, "Alerts": len(data.Alerts)}
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
		case !p.Healthy || used*pctFull >= redFrom(cfg.WarnPct):
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
