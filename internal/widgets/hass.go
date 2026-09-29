package widgets

import (
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// hassSwitchable are domains the toggle service works on.
var hassSwitchable = map[string]bool{"switch": true, "light": true, "input_boolean": true, "fan": true, "automation": true}

// HassConfig lists the entities a "hass" widget shows, in order.
type HassConfig struct {
	Entities   []string
	Labels     map[string]string // own names by lower-case entity ID
	Thresholds []Threshold       // by entity ID or shown name
	TwoCols    bool
}

func decodeHass(raw map[string]any) any {
	return HassConfig{Entities: asStringList(raw["entities"]), Labels: parsePairs(asString(raw["labels"])),
		Thresholds: parseThresholds(asString(raw["thresholds"])), TwoCols: asBool(raw["two_columns"])}
}

// HassRow is one entity line; Toggle offers a switch, On is its state.
type HassRow struct {
	ID, Name, Value, Unit string
	Toggle, On            bool
	Missing               bool
	Low                   bool   // a battery below batteryLow
	Level                 string // from the tile's thresholds: warn, fail
}

// batteryLow is the charge (%) below which a battery shows red.
const batteryLow = 15

// HassToggleable reports whether a configured entity can be switched.
func HassToggleable(cfg any, entityID string) bool {
	c, ok := cfg.(HassConfig)
	if !ok {
		return false
	}
	domain, _, _ := strings.Cut(entityID, ".")
	for _, id := range c.Entities {
		if id == entityID {
			return hassSwitchable[domain]
		}
	}
	return false
}

func hassView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(HassConfig)
	data, ok := results["data"].(*sources.HassDataset)
	if !ok {
		return map[string]any{}
	}
	rows := make([]HassRow, 0, len(cfg.Entities))
	for _, id := range cfg.Entities {
		e, found := data.Find(id)
		if !found {
			name := id
			if own := cfg.Labels[strings.ToLower(id)]; own != "" {
				name = own
			}
			rows = append(rows, HassRow{ID: id, Name: name, Missing: true})
			continue
		}
		row := HassRow{ID: id, Name: e.Name, Value: e.State, Unit: e.Unit, Toggle: hassSwitchable[e.Domain], On: e.State == sources.HassOn}
		if own := cfg.Labels[strings.ToLower(id)]; own != "" {
			row.Name = own
		}
		if n, err := strconv.ParseFloat(e.State, 64); err == nil {
			row.Low = e.DeviceClass == "battery" && n < batteryLow
			row.Level = levelOf(id, n, cfg.Thresholds)
			if row.Level == "" {
				row.Level = levelOf(row.Name, n, cfg.Thresholds)
			}
		}
		rows = append(rows, row)
	}
	return map[string]any{"Rows": rows, "TwoCols": cfg.TwoCols}
}

func init() {
	Register(WidgetType{Key: "hass", Decode: decodeHass, Category: CategoryStart,
		Service: enums.ServiceHomeAssistant, RefreshS: 60, Live: true, Queries: dataQuery, View: hassView})
}
