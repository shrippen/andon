package widgets

// "status_light": one state for the wall display, from the open hints and
// the tile's own thresholds, and below it only what is not green.
//
//	red_from: critical | warn        yellow_from: warn | info | off

import "andon/internal/enums"

// HintsSlot carries the open hints (as []HintBrief) for ExtraHintBriefs.
const HintsSlot = "hints"

// HintBrief is one open hint as a tile needs it.
type HintBrief struct {
	ID       int64
	Severity enums.Severity
	Title    string
	Sources  []string
}

// HintSource is a config that picks hints (see ExtraHints, ExtraHintBriefs).
type HintSource interface{ Hints() HintsConfig }

// Hints makes HintsConfig its own hint filter.
func (c HintsConfig) Hints() HintsConfig { return c }

// StatusLightConfig is the "status_light" widget's config; a zero Yellow
// means yellow is off.
type StatusLightConfig struct {
	Red, Yellow enums.Severity
	Sources     []string
}

// lightLevels maps the choices to severities; "off" is 0.
var lightLevels = map[string]enums.Severity{"critical": enums.SeverityCritical, "warn": enums.SeverityWarn, "info": enums.SeverityInfo, "off": 0}

const lightShown = 5

func decodeStatusLight(raw map[string]any) any {
	level := func(key, def string) enums.Severity {
		if v, ok := lightLevels[asString(raw[key])]; ok {
			return v
		}
		return lightLevels[def]
	}
	return StatusLightConfig{Red: max(level("red_from", "critical"), enums.SeverityWarn), Yellow: level("yellow_from", "warn"),
		Sources: asStringList(raw["sources"])}
}

// Hints loads every hint that can colour the light.
func (c StatusLightConfig) Hints() HintsConfig {
	floor := c.Red
	if c.Yellow > 0 {
		floor = min(c.Red, c.Yellow)
	}
	return HintsConfig{MinSeverity: int(floor), Sources: c.Sources}
}

func statusLightView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(StatusLightConfig)
	briefs, _ := results[HintsSlot].([]HintBrief)
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
	return map[string]any{"State": state, "Items": items[:min(len(items), lightShown)], "More": more, "Count": len(items)}
}

func init() {
	Register(WidgetType{Key: "status_light", Decode: decodeStatusLight, Template: "widgets/status_light", Category: CategoryInsight,
		RefreshS: 60, View: statusLightView, Extra: ExtraHintBriefs})
}
