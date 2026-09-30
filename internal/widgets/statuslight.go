package widgets

// "status_light": one state for the wall display, from the open hints and
// the tile's own thresholds, and below it only what is not green.
//
//	red_from: critical | warn        yellow_from: warn | info | off

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// HintsSlot carries the open hints (as []HintBrief) for ExtraHintBriefs.
const HintsSlot = "hints"

// HintBrief is one open hint as a tile needs it.
type HintBrief struct {
	ID       int64
	Severity enums.Severity
	Title    string
	Sources  []string
	Note     string // catalog key after the title, for lines that are not hints
}

// ConnHealthWanter is a config that wants the connection strips too
// (results[ConnHealthSlot]) besides its own extra.
type ConnHealthWanter interface{ WantsConnHealth() bool }

// HintSource is a config that picks hints (see ExtraHints, ExtraHintBriefs).
type HintSource interface{ Hints() HintsConfig }

// Hints makes HintsConfig its own hint filter.
func (c HintsConfig) Hints() HintsConfig { return c }

// StatusLightConfig is the "status_light" widget's config; a zero Yellow
// means yellow is off.
type StatusLightConfig struct {
	Red, Yellow enums.Severity
	Sources     []string
	Direct      bool              // monitors down and failing connections turn it red themselves
	Texts       map[string]string // own words for green, yellow, red
}

// WantsConnHealth: only a direct light reads the connections.
func (c StatusLightConfig) WantsConnHealth() bool { return c.Direct }

// lightLevels maps the choices to severities; "off" is 0.
var lightLevels = map[string]enums.Severity{"critical": enums.SeverityCritical, "warn": enums.SeverityWarn, "info": enums.SeverityInfo, "off": 0}

const lightShown = 5

func decodeStatusLight(r Raw) StatusLightConfig {
	texts := map[string]string{}
	for _, state := range []string{"green", "yellow", "red"} {
		if t := strings.TrimSpace(r.String("text_" + state)); t != "" {
			texts[state] = t
		}
	}
	return StatusLightConfig{Red: lightLevels[r.Pick("red_from")], Yellow: lightLevels[r.Pick("yellow_from")],
		Sources: r.Lower("sources"), Direct: r.Bool("direct"), Texts: texts}
}

// Hints loads every hint that can colour the light.
func (c StatusLightConfig) Hints() HintsConfig {
	floor := c.Red
	if c.Yellow > 0 {
		floor = min(c.Red, c.Yellow)
	}
	return HintsConfig{MinSeverity: int(floor), Sources: c.Sources}
}

// directBriefs are the monitors down and the connections failing today,
// as red lines.
func directBriefs(results map[string]any) []HintBrief {
	var out []HintBrief
	if kuma, ok := results[peerKuma].(*sources.KumaDataset); ok {
		for _, m := range kuma.Monitors {
			if m.Status == sources.KumaDown {
				out = append(out, HintBrief{Severity: enums.SeverityCritical, Title: m.Name, Note: "light.monitor_down"})
			}
		}
	}
	strips, _ := results[ConnHealthSlot].([]ConnStrip)
	for _, s := range strips {
		if n := len(s.Days); n > 0 && s.Days[n-1].Fail > s.Days[n-1].OK {
			out = append(out, HintBrief{Severity: enums.SeverityCritical, Title: s.Name, Note: "light.conn_failing"})
		}
	}
	return out
}

// peerKuma names Uptime Kuma's data for a direct status light.
const peerKuma = "kuma"

func statusLightQueries(cfg StatusLightConfig) []Query {
	if cfg.Direct {
		return []Query{peer(peerKuma, enums.ServiceUptimeKuma)}
	}
	return nil
}

func statusLightView(cfg StatusLightConfig, results map[string]any, _ ViewCtx) map[string]any {
	briefs, _ := results[HintsSlot].([]HintBrief)
	if cfg.Direct {
		briefs = append(directBriefs(results), briefs...)
	}
	state := "green"
	var items []HintBrief
	for _, b := range briefs {
		switch {
		case b.Severity >= cfg.Red:
			state = "red"
		case cfg.Yellow > 0 && b.Severity >= cfg.Yellow:
			if state == "green" {
				state = "yellow"
			}
		default:
			continue
		}
		items = append(items, b)
	}
	more := max(len(items)-lightShown, 0)
	return map[string]any{"State": state, "Items": items[:min(len(items), lightShown)], "More": more, "Count": len(items), "Text": cfg.Texts[state]}
}

func init() {
	Tile[StatusLightConfig]{Key: "status_light", Category: CategoryInsight, Topic: TopicOverview, RefreshS: 60, Extra: ExtraHintBriefs,
		Fields: []Field{sel("red_from", "critical", "critical", "warn"), sel("yellow_from", "warn", "warn", "info", "off"), {Key: "sources", Input: InputList},
			{Key: "direct", Input: InputCheck}, {Key: "text_green", Input: InputText}, {Key: "text_yellow", Input: InputText}, {Key: "text_red", Input: InputText}},
		Decode: decodeStatusLight, Queries: statusLightQueries, View: statusLightView,
		Calm: func(v map[string]any) bool { return v["State"] == "green" }}.add()
}
