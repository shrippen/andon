package widgets

// Homelab widgets across services (phase 13):
//
//	update_window     pending updates against backup age, streams, timers,
//	                  appointments and the power price
//	storage_forecast  every storage's share and days until full (history)
//	table exposure    Pangolin's public resources: login, certificate, updates
//	table domain_chain what depends on each domain

import (
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	// HistorySlot names the recorded history among a widget's results.
	HistorySlot = "history"

	TableExposure    TableKind = "exposure"
	TableDomainChain TableKind = "domain_chain"

	windowBackupAge = 24 * time.Hour
	windowRefreshS  = 300
)

// windowPeers are the services the update window weighs.
var windowPeers = []enums.ServiceType{
	enums.ServiceKimai, enums.ServiceMediaServer, enums.ServiceCalendar, enums.ServiceTibber,
	enums.ServiceBorgBackup, enums.ServiceTrueNAS, enums.ServiceProxmox, enums.ServiceImmich, enums.ServiceAuthentik,
	enums.ServiceKomodo, enums.ServiceNextcloud, enums.ServiceGateway, enums.ServiceHomeAssistant,
}

// updatePeers report pending updates for the exposure table.
var updatePeers = []enums.ServiceType{enums.ServiceKomodo, enums.ServiceTrueNAS, enums.ServiceImmich, enums.ServiceAuthentik, enums.ServiceNextcloud}

func peersOf(services []enums.ServiceType) []Query {
	out := make([]Query, 0, len(services))
	for _, s := range services {
		out = append(out, peer(string(s), s))
	}
	return out
}

// peerDatasets collects the results of peer queries by service.
func peerDatasets(results map[string]any, services []enums.ServiceType) map[string]any {
	out := map[string]any{}
	for _, s := range services {
		if d, ok := results[string(s)]; ok {
			out[string(s)] = d
		}
	}
	return out
}

// WindowConfig is the "update_window" widget's config.
type WindowConfig struct {
	From, To int             // maintenance window in minutes of the day; From == To = always
	Timezone string          // of the window
	Ignore   map[string]bool // blockers left out (metrics.Window* keys)
}

// windowFactors maps each blocker to the checkbox that weighs it.
var windowFactors = map[string]string{metrics.WindowNoBackup: "use_backup", metrics.WindowStreaming: "use_streams",
	metrics.WindowWorking: "use_timer", metrics.WindowMeeting: "use_meetings", metrics.WindowExpensive: "use_price"}

// WindowOutside is the blocker for "outside the maintenance window".
const WindowOutside = "outside"

func init() {
	Tile[WindowConfig]{Key: "update_window", Detail: updateWindowDetail, Category: CategoryInsight, Topic: TopicHomelab, RefreshS: windowRefreshS,
		Fields: []Field{{Key: "window", Input: InputText}, {Key: "timezone", Input: InputText, Default: defaultTimezone},
			{Key: "use_backup", Input: InputCheck, Default: true}, {Key: "use_streams", Input: InputCheck, Default: true},
			{Key: "use_timer", Input: InputCheck, Default: true}, {Key: "use_meetings", Input: InputCheck, Default: true},
			{Key: "use_price", Input: InputCheck, Default: true}},
		Decode: decodeWindow, Queries: func(WindowConfig) []Query { return peersOf(windowPeers) }, View: updateWindowView,
		// A window "25-3" would mean "always", a zone "Europe/Berln" UTC.
		Check: func(raw map[string]any) string {
			if w := strings.TrimSpace(asString(raw["window"])); w != "" {
				if _, _, ok := parseSpan(w); !ok {
					return CheckBadWindow
				}
			}
			return checkZone(raw)
		}}.add()
}

func decodeWindow(r Raw) WindowConfig {
	// An empty zone is the default too, not only a missing one.
	cfg := WindowConfig{Ignore: map[string]bool{}, Timezone: r.String("timezone")}
	if cfg.Timezone == "" {
		cfg.Timezone = defaultTimezone
	}
	cfg.From, cfg.To, _ = parseSpan(r.String("window"))
	for blocker, box := range windowFactors {
		if !r.Bool(box) {
			cfg.Ignore[blocker] = true
		}
	}
	return cfg
}

// parseSpan reads "22:00-06:00" or "22-6" into minutes of the day.
func parseSpan(s string) (int, int, bool) {
	from, to, ok := strings.Cut(strings.ReplaceAll(s, " ", ""), "-")
	if !ok {
		return 0, 0, false
	}
	a, okA := clockOf(from)
	b, okB := clockOf(to)
	return a, b, okA && okB
}

func clockOf(s string) (int, bool) {
	h, m, _ := strings.Cut(s, ":")
	hours, err := strconv.Atoi(h)
	if err != nil || hours < 0 || hours > 24 {
		return 0, false
	}
	minutes := 0
	if m != "" {
		if minutes, err = strconv.Atoi(m); err != nil || minutes < 0 || minutes > 59 {
			return 0, false
		}
	}
	return hours*minutesPerHour + minutes, true
}

// inSpan: the minute of the day lies in [from, to), across midnight too.
func inSpan(minute, from, to int) bool {
	if from == to {
		return true
	}
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to
}

func updateWindowView(cfg WindowConfig, results map[string]any, ctx ViewCtx) map[string]any {
	now := time.Now().UTC()
	w := metrics.UpdateWindow(peerDatasets(results, windowPeers), now, windowBackupAge)
	kept := w.Blockers[:0]
	for _, b := range w.Blockers {
		if !cfg.Ignore[b] {
			kept = append(kept, b)
		}
	}
	w.Blockers = kept
	if cfg.From != cfg.To {
		loc, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			loc = time.UTC
		}
		local := now.In(loc)
		if !inSpan(local.Hour()*minutesPerHour+local.Minute(), cfg.From, cfg.To) {
			w.Blockers = append(w.Blockers, WindowOutside)
		}
	}
	return map[string]any{"Window": w, "Good": len(w.Blockers) == 0 && len(w.Updates) > 0,
		"BackupHours": int(w.BackupAge.Hours())}
}

// StorageRow is one store as drawn: the used part, how much more the next
// storageAhead days add at the recent pace (both in percent), and when
// it is full.
type StorageRow struct {
	Label        string
	Used, Ahead  float64
	FullIn       int    // days, -1 = not filling
	FullOn, Tier string // Tier: red within system.storage_forecast's critical days, yellow within its warning days
}

const storageAhead = 30

// fullTier colours days until full with system.storage_forecast's
// limits: red within its critical days, yellow within its warning days.
func fullTier(fullIn int, ctx ViewCtx) string {
	switch {
	case fullIn < 0:
		return ""
	case float64(fullIn) < rules.Setting(ctx.Settings, "system.storage_forecast", "critical_days"):
		return "red"
	case float64(fullIn) < rules.Setting(ctx.Settings, "system.storage_forecast", "warn_days"):
		return "yellow"
	}
	return ""
}

// StorageConfig is the "storage_forecast" widget's config.
type StorageConfig struct {
	Only  []string // label parts to keep (lower case), empty = all
	Ahead int      // days the dashed part looks ahead
}

func init() {
	Tile[StorageConfig]{Key: "storage_forecast", Detail: storageDetail, Category: CategoryInsight, Topic: TopicHomelab, RefreshS: 3600, Extra: ExtraHistory,
		Fields: []Field{{Key: "filter", Input: InputList}, {Key: "ahead", Input: InputNumber, Default: storageAhead, Min: "1", Max: "365"}},
		Decode: func(r Raw) StorageConfig { return StorageConfig{Only: r.Lower("filter"), Ahead: r.Int("ahead")} },
		View:   storageView}.add()
}

// matchesAny: no filter, or the name contains one of the parts.
func matchesAny(name string, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	name = strings.ToLower(name)
	for _, p := range parts {
		if p != "" && strings.Contains(name, p) {
			return true
		}
	}
	return false
}

func storageView(cfg StorageConfig, results map[string]any, ctx ViewCtx) map[string]any {
	if cfg.Ahead == 0 {
		cfg.Ahead = storageAhead
	}
	h, _ := results[HistorySlot].(*metrics.History)
	if h == nil {
		return map[string]any{}
	}
	today := todayOf(ctx)
	var rows []StorageRow
	for _, f := range metrics.StorageForecasts(h, today) {
		if !matchesAny(f.Label, cfg.Only) {
			continue
		}
		row := StorageRow{Label: f.Label, Used: f.Used * pctFull, FullIn: f.FullIn}
		if f.FullIn >= 0 {
			row.FullOn = today.AddDate(0, 0, f.FullIn).Format(time.DateOnly)
			perDay := (1 - f.Used) / float64(max(f.FullIn, 1))
			row.Ahead = min(perDay*float64(cfg.Ahead), 1-f.Used) * pctFull
		}
		row.Tier = fullTier(f.FullIn, ctx)
		rows = append(rows, row)
	}
	return map[string]any{"Rows": rows, "Ahead": cfg.Ahead}
}

// homelabQueries are the peers of the homelab tables.
func homelabQueries(kind TableKind) []Query {
	switch kind {
	case TableExposure:
		return append(peersOf(updatePeers), peer(string(enums.ServiceCerts), enums.ServiceCerts))
	case TableDomainChain:
		return peersOf([]enums.ServiceType{enums.ServiceCerts, enums.ServicePangolin, enums.ServiceUptimeKuma})
	}
	return nil
}

func homelabCols(kind TableKind) []Col {
	switch kind {
	case TableExposure:
		return []Col{{"name", "text"}, {"domain", "text"}, {"login", "yesno"}, {"cert_days", "text"}, {"updates", "text"}, {"risk", "risk"}}
	case TableDomainChain:
		return []Col{{"domain", "text"}, {"expires", "day"}, {"cert_days", "text"}, {"resources", "text"}, {"monitors", "text"}}
	}
	return nil
}

func homelabRows(kind TableKind, data any, results map[string]any, ctx ViewCtx) ([]Row, bool) {
	today := todayOf(ctx)
	var rows []Row
	switch d := data.(type) {
	case *sources.PangolinDataset:
		if kind != TableExposure {
			return nil, false
		}
		certs, _ := results[string(enums.ServiceCerts)].(*sources.CertDataset)
		updates := metrics.PendingUpdates(peerDatasets(results, updatePeers))
		for _, r := range metrics.Exposure(d, certs, updates, today) {
			rows = append(rows, Row{[]any{r.Name, r.Domain, r.Login, certText(r.CertDays), strings.Join(r.Updates, ", "), r.Risk}})
		}
	case *sources.DomainsDataset:
		if kind != TableDomainChain {
			return nil, false
		}
		certs, _ := results[string(enums.ServiceCerts)].(*sources.CertDataset)
		pangolin, _ := results[string(enums.ServicePangolin)].(*sources.PangolinDataset)
		kuma, _ := results[string(enums.ServiceUptimeKuma)].(*sources.KumaDataset)
		for _, c := range metrics.DomainChains(d, certs, pangolin, kuma, nil, today) {
			rows = append(rows, Row{[]any{c.Domain, dayOrEmpty(c.Expires), certText(c.CertDays), strings.Join(c.Resources, ", "), strings.Join(c.Monitors, ", ")}})
		}
	default:
		return nil, false
	}
	return rows, true
}

// certText shows remaining certificate days, "" when unknown.
func certText(days int) string {
	if days < 0 {
		return ""
	}
	return strconv.Itoa(days)
}
