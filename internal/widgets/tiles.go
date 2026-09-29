package widgets

// Data tiles drawn as bars (see andon.css .bars, .seg, .stack):
//
//	conn_health    every connection's last days as a strip, worst first
//	invoice_aging  open invoices stacked by how overdue they are

import (
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// LoadBar is one column of a load bar chart: height in percent and a colour band.
type LoadBar struct {
	H     int
	Tier  string // "", "yellow", "red"
	Title string
}

// Tier bands of percent loads (CPU, fill levels).
const (
	loadWarn = 60
	loadHigh = 80
)

func loadTier(pct float64) string {
	switch {
	case pct >= loadHigh:
		return "red"
	case pct >= loadWarn:
		return "yellow"
	default:
		return ""
	}
}

// barsOf buckets values into at most n bars (averaging neighbours) and
// scales them to high:
//
//	[10 20 30 40] n=2 high=40  →  heights 38 88
func barsOf(values []float64, n int, high float64) []LoadBar {
	if len(values) == 0 || n <= 0 || high <= 0 {
		return nil
	}
	per := max(1, (len(values)+n-1)/n)
	var out []LoadBar
	for i := 0; i < len(values); i += per {
		end := min(i+per, len(values))
		sum := 0.0
		for _, v := range values[i:end] {
			sum += v
		}
		avg := sum / float64(end-i)
		out = append(out, LoadBar{H: max(2, int(min(avg, high)/high*pctFull+0.5)), Tier: loadTier(avg / high * pctFull)})
	}
	return out
}

// ── conn_health ──

// ConnHealthSlot names the connection strips among a widget's results.
const ConnHealthSlot = "connhealth"

// Days a connection strip covers.
const ConnHealthDays = 14

// ConnHealthConfig is the "conn_health" widget's config.
type ConnHealthConfig struct {
	Limit int
	Now   bool // only connections failing in the last days, not long ago
}

// connShakyDays is how far back "failing now" looks.
const connShakyDays = 2

func decodeConnHealth(raw map[string]any) any {
	return ConnHealthConfig{Limit: clampInt(asInt(raw["limit"], 4), 1, 20), Now: asBool(raw["only_problems"])}
}

// failingLately: a failure within the last connShakyDays days.
func failingLately(s ConnStrip) bool {
	for _, d := range s.Days[max(len(s.Days)-connShakyDays, 0):] {
		if d.Fail > 0 {
			return true
		}
	}
	return false
}

// ConnStrip is one connection's strip as the services hand it over.
type ConnStrip struct {
	Name, Service string
	FailPct       int
	Days          []ConnDayState
}

// ConnDayState is one day of a strip.
type ConnDayState struct {
	Day      string
	OK, Fail int
}

// StripCell is one day as drawn: "ok", "mid" (some failures), "bad"
// (mostly failures) or "none" (no fetch).
type StripCell struct {
	State string
	Title string
}

// StripRow is one connection as drawn.
type StripRow struct {
	Name    string
	FailPct int
	Cells   []StripCell
	lately  bool // failed within connShakyDays
}

func cellState(d ConnDayState) string {
	switch {
	case d.OK+d.Fail == 0:
		return "none"
	case d.Fail == 0:
		return "ok"
	case d.Fail < d.OK:
		return "mid"
	default:
		return "bad"
	}
}

// connHealthView counts healthy connections and draws the worst ones.
func connHealthView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(ConnHealthConfig)
	strips, _ := results[ConnHealthSlot].([]ConnStrip)

	healthy := 0
	var rows []StripRow
	for _, s := range strips {
		if s.FailPct == 0 {
			healthy++
		}
		row := StripRow{Name: s.Name, FailPct: s.FailPct, lately: failingLately(s)}
		for _, d := range s.Days {
			row.Cells = append(row.Cells, StripCell{State: cellState(d), Title: d.Day})
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].FailPct > rows[b].FailPct })

	var shown []StripRow
	for _, r := range rows {
		if r.FailPct > 0 && len(shown) < cfg.Limit && (!cfg.Now || r.lately) {
			shown = append(shown, r)
		}
	}
	return map[string]any{"Healthy": healthy, "Total": len(strips), "Rows": shown, "Days": ConnHealthDays}
}

// ── invoice_aging ──

// AgingBand is one part of the stacked open-invoice bar.
type AgingBand struct {
	Key      string // catalog suffix: "current", "d30", "d60", "older"
	Tier     string
	Amount   float64
	Count    int
	Pct      int
	From, To int // days of the band
	Over     int // the open-ended band: more than this many days
}

// AgingConfig is the config of the aging tiles: two band limits in days
// and names left out.
type AgingConfig struct {
	Mid, Old     int
	HideClients  []string // lower case
	HideInternal bool     // unbilled: the space's internal customers
}

func decodeAging(def [2]int) DecodeFunc {
	return func(raw map[string]any) any {
		cfg := AgingConfig{Mid: def[0], Old: def[1], HideInternal: asBool(raw["hide_internal"])}
		if b := asIntList(raw["bands"]); len(b) == 2 && b[0] > 0 && b[1] > b[0] {
			cfg.Mid, cfg.Old = b[0], b[1]
		}
		cfg.HideClients = lowerList(raw["hide_clients"])
		return cfg
	}
}

func asAnyList(v any) []any {
	list, _ := v.([]any)
	return list
}

// agingLimits are the upper overdue days of the bands after "current".
var agingLimits = []struct {
	key, tier string
	upTo      int
}{
	{"current", "green", 0}, {"d30", "yellow", 30}, {"d60", "orange", 60}, {"older", "red", -1},
}

// invoiceAgingView stacks open invoices by days overdue:
//
//	not due 2.940 € · 1–30 d 1.240 € · 31–60 d 0 · older 0
func invoiceAgingView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, ok := cfgAny.(AgingConfig)
	if !ok {
		cfg = decodeAging([2]int{30, 60})(nil).(AgingConfig)
	}
	data, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return map[string]any{}
	}
	today, _ := time.Parse(time.DateOnly, ctx.Today)
	var open []metrics.NinjaOpenInvoice
	for _, inv := range metrics.NinjaOpenInvoices(data, today) {
		if !slices.Contains(cfg.HideClients, strings.ToLower(inv.Client)) {
			open = append(open, inv)
		}
	}

	limits := []int{0, cfg.Mid, cfg.Old, -1}
	bands := make([]AgingBand, len(agingLimits))
	total := 0.0
	for i, l := range agingLimits {
		bands[i] = AgingBand{Key: l.key, Tier: l.tier}
		if i > 0 {
			bands[i].From, bands[i].To = limits[i-1]+1, max(limits[i], 0)
		}
		if limits[i] < 0 {
			bands[i].Over = limits[i-1]
		}
	}
	for _, inv := range open {
		i := len(limits) - 1
		for j, upTo := range limits {
			if upTo >= 0 && inv.OverdueDays <= upTo {
				i = j
				break
			}
		}
		bands[i].Amount += inv.Balance
		bands[i].Count++
		total += inv.Balance
	}
	for i := range bands {
		if total > 0 {
			bands[i].Pct = int(bands[i].Amount/total*pctFull + 0.5)
		}
	}
	return map[string]any{"Total": total, "Count": len(open), "Bands": bands}
}

// ── speedtest ──

// speedView sets the last measurement against the contract.
// SpeedConfig is the "speedtest" widget's config.
type SpeedConfig struct{ Ping bool }

func decodeSpeed(raw map[string]any) any { return SpeedConfig{Ping: boolOr(raw["ping"], true)} }

func speedView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(SpeedConfig)
	if !ok {
		cfg.Ping = true
	}
	data, ok := results["data"].(*sources.SpeedtestDataset)
	if !ok || data == nil {
		return map[string]any{}
	}
	out := map[string]any{"Data": data, "Ping": cfg.Ping}
	if data.ExpectDown > 0 {
		out["DownPct"] = data.Down / data.ExpectDown
	}
	if data.ExpectUp > 0 {
		out["UpPct"] = data.Up / data.ExpectUp
	}
	return out
}

func init() {
	Register(WidgetType{Key: "conn_health", Decode: decodeConnHealth, Template: "widgets/conn_health", Category: CategoryInsight,
		RefreshS: 10 * 60, View: connHealthView, Extra: ExtraConnHealth})
	Register(WidgetType{Key: "invoice_aging", Decode: decodeAging([2]int{30, 60}), Template: "widgets/invoice_aging", Category: CategoryInsight,
		Service: enums.ServiceInvoiceNinja, RefreshS: 30 * 60, Queries: dataQuery, View: invoiceAgingView})
}

// lowerList reads a list field in lower case, for names compared loosely.
func lowerList(v any) []string {
	var out []string
	for _, n := range asStringList(v) {
		out = append(out, strings.ToLower(n))
	}
	return out
}
