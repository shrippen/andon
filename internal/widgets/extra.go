package widgets

// Widgets added for Dashy imports: image, exchange rates, and the
// monitor list of an Uptime Kuma connection.

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

const (
	defaultImageHeight = 240
	defaultRatesBase   = "EUR"
)

// ImageConfig is the "image" widget's config; Link is "" for none.
type ImageConfig struct {
	URL     string
	Height  int
	Link    string
	ReloadM int  // minutes between fresh loads (webcams), 0 = the default hour
	Cover   bool // fill the box (and crop) instead of showing the whole picture
}

func decodeImage(r Raw) ImageConfig {
	return ImageConfig{URL: r.URL("url"), Height: r.Int("height"), Link: r.URL("link"), ReloadM: r.Int("reload"), Cover: r.Pick("fit") == "cover"}
}

// RefreshSeconds lets the tile reload at the chosen pace.
func (c ImageConfig) RefreshSeconds() int { return c.ReloadM * secondsPerMinute }

// Refresher is a config that sets its tile's refresh itself.
type Refresher interface{ RefreshSeconds() int }

// Detailer is a config whose dialog depends on its settings: one clock
// zone has nothing to compare.
type Detailer interface{ OffersDetail() bool }

const secondsPerMinute = 60

// freshBucket names the current period of a reload pace, so a query with
// it misses the cache once per period: 0 when there is no pace.
func freshBucket(periodS int) float64 {
	if periodS <= 0 {
		return 0
	}
	return float64(time.Now().Unix() / int64(periodS))
}

// RatesConfig is the "rates" widget's config.
type RatesConfig struct {
	Base    string
	Symbols []string
	Change  bool // against the previous day
	Invert  bool // 1 USD = … EUR instead of 1 EUR = … USD
}

// defaultRates are the currencies a new rates tile lists.
var defaultRates = []string{"USD", "CHF", "GBP"}

func decodeRates(r Raw) RatesConfig {
	return RatesConfig{Base: textOr(r, "base"), Symbols: listOr(r, "symbols"), Change: r.Bool("change"), Invert: r.Bool("invert")}
}

// MonitorRow is one Kuma monitor with its pill state and label key.
type MonitorRow struct {
	Name, State, Key string
}

// kumaStates maps monitor_status to pill state and "kuma.<key>" label.
var kumaStates = map[int][2]string{
	sources.KumaDown:        {"fail", "down"},
	sources.KumaUp:          {"ok", "up"},
	sources.KumaPending:     {"warn", "pending"},
	sources.KumaMaintenance: {"warn", "maintenance"},
}

// kumaCells maps monitor_status to a strip cell state.
var kumaCells = map[int]string{sources.KumaDown: "bad", sources.KumaUp: "ok", sources.KumaPending: "mid", sources.KumaMaintenance: "none"}

// MonitorLine is one monitor with its last days, as Andon saw them.
type MonitorLine struct {
	Name, State string // State: pill state now
	MS          float64
	Days        []StripCell
}

// Uptime tiers of a day cell: at least uptimeOK is "ok", uptimeWarn "mid".
const (
	uptimeOK   = 0.999
	uptimeWarn = 0.95
	uptimeDays = 14
	monitorMax = 8
)

// monitorLines draws each monitor's last uptimeDays days, in the order
// given (down first); nil before the first recorded run.
func monitorLines(mons []sources.KumaMonitor, h *metrics.History, today time.Time, days int) []MonitorLine {
	var out []MonitorLine
	recorded := false
	for _, m := range mons[:min(len(mons), monitorMax)] {
		line := MonitorLine{Name: m.Name, State: kumaStates[m.Status][0], MS: m.MS}
		for i, share := range metrics.UptimeDays(h, m.Name, today, days) {
			day := today.AddDate(0, 0, i-days+1).Format(time.DateOnly)
			cell := StripCell{State: "none", Title: day}
			switch {
			case share < 0:
			case share >= uptimeOK:
				cell.State = "ok"
			case share >= uptimeWarn:
				cell.State = "mid"
			default:
				cell.State = "bad"
			}
			if share >= 0 {
				recorded = true
				cell.Title = fmt.Sprintf("%s · %.1f %%", day, share*pctFull)
			}
			line.Days = append(line.Days, cell)
		}
		out = append(out, line)
	}
	if !recorded {
		return nil
	}
	return out
}

// monitorProblems is how many problems the monitors tile lists.
const monitorProblems = 4

// monitorsView sums up the instance in a few lines: how many are up, one
// strip cell per monitor (down first) and only the monitors that are not
// up by name.
//
//	12 / 14 online · Ø 180 ms
//	▮▮▮▮▮▮▮▮▮▮▮▮▯▯
//	NAS down · Shop pending
//
// MonitorsConfig is the "monitors" widget's config.
type MonitorsConfig struct {
	Only   []string // monitor name parts (lower case), empty = all
	Days   int
	HideMS bool
}

func decodeMonitors(r Raw) MonitorsConfig {
	days, _ := strconv.Atoi(r.Pick("days"))
	return MonitorsConfig{Only: r.Lower("filter"), Days: days, HideMS: !r.Bool("response_time")}
}

func monitorsView(cfg MonitorsConfig, results map[string]any, ctx ViewCtx) map[string]any {
	certNotice := rules.Setting(ctx.Settings, "kuma.cert_expiring", "info_days")
	// No config (a nil one): the default days.
	if cfg.Days == 0 {
		cfg.Days = uptimeDays
	}
	data, ok := results["data"].(*sources.KumaDataset)
	if !ok {
		return map[string]any{}
	}
	var mons []sources.KumaMonitor
	for _, m := range data.Monitors {
		if matchesAny(m.Name, cfg.Only) {
			mons = append(mons, m)
		}
	}
	rank := func(status int) int {
		if status == sources.KumaUp {
			return 1
		}
		return 0
	}
	sort.SliceStable(mons, func(a, b int) bool { return rank(mons[a].Status) < rank(mons[b].Status) })

	up, msSum, msCount, more := 0, 0.0, 0, 0
	var cells []StripCell
	var problems []MonitorRow
	soon := sources.KumaMonitor{CertDays: -1}
	for _, m := range mons {
		cells = append(cells, StripCell{State: kumaCells[m.Status], Title: m.Name})
		if m.Status == sources.KumaUp {
			up++
			if m.MS > 0 {
				msSum, msCount = msSum+m.MS, msCount+1
			}
		} else if len(problems) < monitorProblems {
			state := kumaStates[m.Status]
			problems = append(problems, MonitorRow{Name: m.Name, State: state[0], Key: "kuma." + state[1]})
		} else {
			more++
		}
		if m.CertDays >= 0 && float64(m.CertDays) < certNotice && (soon.CertDays < 0 || m.CertDays < soon.CertDays) {
			soon = m
		}
	}

	out := map[string]any{"Up": up, "Total": len(mons), "Cells": cells, "Problems": problems, "More": more}
	if h, ok := results[HistorySlot].(*metrics.History); ok {
		if lines := monitorLines(mons, h, todayOf(ctx), cfg.Days); lines != nil {
			if cfg.HideMS {
				for i := range lines {
					lines[i].MS = 0
				}
			}
			out["Lines"], out["LinesMore"] = lines, max(len(mons)-monitorMax, 0)
		}
	}
	if msCount > 0 && !cfg.HideMS {
		out["AvgMS"] = msSum / float64(msCount)
	}
	out["HideMS"] = cfg.HideMS
	if soon.CertDays >= 0 {
		out["CertName"], out["CertDays"] = soon.Name, soon.CertDays
	}
	return out
}

func init() {
	Tile[ImageConfig]{Key: "image", Detail: imageDetail, Category: CategoryStart, Topic: TopicMedia, RefreshS: 60 * 60,
		Fields: []Field{{Key: "url", Input: InputText, Required: true}, {Key: "link", Input: InputText},
			{Key: "height", Input: InputNumber, Default: defaultImageHeight, Min: "40", Max: "1200"}, sel("fit", "contain", "contain", "cover"),
			{Key: "reload", Input: InputNumber, Min: "0", Max: "1440"}},
		Decode: decodeImage, Queries: func(cfg ImageConfig) []Query {
			return []Query{{Name: "image", Source: "image", Params: map[string]any{"url": cfg.URL, "fresh": freshBucket(cfg.ReloadM * secondsPerMinute)}}}
		}}.add()

	Tile[RatesConfig]{Key: "rates", Detail: ratesDetail, Category: CategoryStart, Topic: TopicWorld, RefreshS: 6 * 60 * 60,
		Fields: []Field{{Key: "base", Input: InputText, Default: defaultRatesBase}, {Key: "symbols", Input: InputList, Default: anyList(defaultRates)},
			{Key: "change", Input: InputCheck}, {Key: "invert", Input: InputCheck}},
		Decode: decodeRates, Queries: func(cfg RatesConfig) []Query {
			return []Query{{Name: "rates", Source: "exchange_rates", Params: map[string]any{"base": cfg.Base, "symbols": cfg.Symbols, "change": cfg.Change}}}
		},
		DetailQueries: func(cfg RatesConfig) []Query {
			return []Query{{Name: openName, Source: "exchange_rates.history", Params: map[string]any{"base": cfg.Base, "symbols": cfg.Symbols}}}
		}}.add()

	Tile[MonitorsConfig]{Key: "monitors", Detail: monitorsDetail, Category: CategoryStart, Topic: TopicHomelab, Service: enums.ServiceUptimeKuma, RefreshS: 60,
		Live: true, DataChoice: true, Extra: ExtraHistory,
		Fields: []Field{{Key: "filter", Input: InputList}, sel("days", "14", "7", "14", "30"), {Key: "response_time", Input: InputCheck, Default: true}},
		Decode: decodeMonitors, Queries: ownData[MonitorsConfig], View: monitorsView,
		Calm: func(v map[string]any) bool { return v["Total"] != nil && v["Up"] == v["Total"] }}.add()
}
