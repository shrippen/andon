package rules

// ESPHome:
//
//	esphome.offline     a node does not answer the dashboard's ping
//	esphome.update      nodes run an older ESPHome than the dashboard
//	cross.esphome_ha    the node answers, but Home Assistant has all its
//	                    entities unavailable (API key, adoption), or the
//	                    other way round: Home Assistant gets values from
//	                    a node the dashboard cannot reach (mDNS)

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var esphomeSvc = string(enums.ServiceESPHome)

// haUnavailable is Home Assistant's state of an entity it cannot reach.
const haUnavailable = "unavailable"

func init() {
	Register("esphome.offline", esphomeSvc, nil, on(esphomeOffline))
	Register("esphome.update", esphomeSvc, nil, on(esphomeUpdate))
	Register("cross.esphome_ha", Cross, nil, esphomeHA)
}

func esphomeOffline(data *sources.ESPHomeDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, d := range data.Devices {
		if d.Online == nil || *d.Online {
			continue
		}
		found = append(found, svcFinding(esphomeSvc, "esphome.offline", "offline:"+d.Name, "esphome.offline", enums.SeverityWarn, data.URL,
			map[string]any{"name": d.Friendly, "address": orDash(d.Address)}))
	}
	return found
}

func esphomeUpdate(data *sources.ESPHomeDataset, _ map[string]any, _ Env) []Finding {
	var names []string
	for _, d := range data.Devices {
		if metrics.ESPBehind(d, data.Version) {
			names = append(names, d.Friendly+" ("+d.Deployed+")")
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(esphomeSvc, "esphome.update", "update:"+data.Version, "esphome.update", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(names), "names": shortList(names), "version": data.Version})}
}

func esphomeHA(_ any, _ map[string]any, env Env) []Finding {
	esp, ok := env.Datasets[esphomeSvc].(*sources.ESPHomeDataset)
	ha, ok2 := env.Datasets[string(enums.ServiceHomeAssistant)].(*sources.HassDataset)
	if !ok || !ok2 {
		return nil
	}
	var found []Finding
	for _, d := range esp.Devices {
		ents := nodeEntities(ha, d.Name)
		if d.Online == nil || len(ents) == 0 {
			continue
		}
		lost := true
		for _, e := range ents {
			lost = lost && e.State == haUnavailable
		}

		msg := ""
		switch {
		case *d.Online && lost:
			msg = "cross.esphome_ha_lost"
		case !*d.Online && !lost:
			msg = "cross.esphome_ping"
		default:
			continue
		}
		found = append(found, Finding{Fingerprint: msg + ":" + d.Name, Severity: enums.SeverityWarn, Message: msg,
			Params:    map[string]any{"name": d.Friendly, "entities": len(ents), "entity": ents[0].ID},
			ActionURL: ha.URL, ActionLabel: "open_in_" + string(enums.ServiceHomeAssistant), Sources: []string{esphomeSvc, string(enums.ServiceHomeAssistant)}})
	}
	return found
}

// nodeEntities are the Home Assistant entities ESPHome named after the
// node: "werkstatt-klima" → sensor.werkstatt_klima_temperatur.
func nodeEntities(ha *sources.HassDataset, node string) []sources.Entity {
	object := strings.ReplaceAll(strings.ToLower(node), "-", "_")
	var out []sources.Entity
	for _, e := range ha.Entities {
		_, id, _ := strings.Cut(e.ID, ".")
		if id == object || strings.HasPrefix(id, object+"_") {
			out = append(out, e)
		}
	}
	return out
}
