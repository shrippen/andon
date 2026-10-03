package rules

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const listShown = 5

// hassAlarms are binary sensors whose "on" means damage now.
var hassAlarms = map[string]bool{"moisture": true, "smoke": true, "gas": true, "carbon_monoxide": true, "safety": true, "problem": true}

// hassSilent are domains where "unavailable" is normal.
var hassSilent = map[string]bool{"update": true, "button": true, "scene": true, "event": true}

// shortList joins names, "A, B, C +2".
func shortList(names []string) string {
	sort.Strings(names)
	if len(names) <= listShown {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:listShown], ", ") + " +" + strconv.Itoa(len(names)-listShown)
}

func init() {
	Register("hass.alarm", hassSvc, nil, on(hassAlarm))

	Register("hass.battery_low", hassSvc, map[string]any{"warn": 20.0, "critical": 10.0}, on(batteryLow))

	// One hint for all long-unavailable entities: a dead Zigbee stick
	// takes dozens with it, and that should read as one problem.
	Register("hass.unavailable", hassSvc, map[string]any{"hours": 6.0, "ignore": []any{}}, on(hassUnavailable))

	Register("hass.updates", hassSvc, nil, on(hassUpdates))
}

// hassEntities links the entity list.
func hassEntities(data *sources.HassDataset) string {
	return strings.TrimRight(data.URL, "/") + "/config/entities"
}

func hassAlarm(data *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, e := range data.Entities {
		if e.Domain != "binary_sensor" || !hassAlarms[e.DeviceClass] || e.State != sources.HassOn {
			continue
		}
		found = append(found, svcFinding(hassSvc, "hass.alarm", "alarm:"+e.ID, "hass.alarm", enums.SeverityCritical,
			data.URL, map[string]any{"entity": e.Name, "kind": e.DeviceClass}))
	}
	return found
}

func batteryLow(data *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, b := range metrics.LowBatteries(data, cfgFloat(cfg, "warn")) {
		sev := enums.SeverityWarn
		if b.Level < cfgFloat(cfg, "critical") {
			sev = enums.SeverityCritical
		}
		found = append(found, svcFinding(hassSvc, "hass.battery_low", "battery:"+b.Entity.ID, "hass.battery", sev,
			hassEntities(data), map[string]any{"entity": b.Entity.Name, "percent": int(b.Level)}))
	}
	return found
}

func hassUnavailable(data *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	ignore := stringsSlice(cfg["ignore"])
	var names []string
	for _, e := range data.Entities {
		if (e.State != sources.HassUnavailable && e.State != sources.HassUnknown) || hassSilent[e.Domain] {
			continue
		}
		if e.Changed.IsZero() || time.Now().UTC().Sub(e.Changed).Hours() < cfgFloat(cfg, "hours") || hasPrefix(e.ID, ignore) {
			continue
		}
		names = append(names, e.Name)
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(hassSvc, "hass.unavailable", "unavailable", "hass.unavailable", enums.SeverityWarn,
		hassEntities(data), map[string]any{"count": len(names), "names": shortList(names)})}
}

func hassUpdates(data *sources.HassDataset, cfg map[string]any, env Env) []Finding {
	var names []string
	for _, e := range data.Entities {
		if e.Domain == "update" && e.State == sources.HassOn {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(hassSvc, "hass.updates", "updates", "hass.updates", enums.SeverityInfo,
		strings.TrimRight(data.URL, "/")+"/config/updates", map[string]any{"count": len(names), "names": shortList(names)})}
}

func hasPrefix(id string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(strings.ToLower(id), p) {
			return true
		}
	}
	return false
}
