package rules

// Power:
//
//	ups.on_battery          a UPS runs on battery (any tool: PeaNUT, apcupsd)
//	ups.runtime_low         a UPS holds less than "minutes" at its load
//	ups.replace_battery     a UPS asks for a new battery
//	opendtu.offline         an inverter is unreachable in daylight
//	cross.charge_expensive  EVCC charges from the grid while Tibber's price is
//	                        "factor" above the day's average
//
// A UPS on battery is also the root of outages (outage.go): the failures
// of every host gather under it.

import (
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

var opendtuSvc = string(enums.ServiceOpenDTU)

// daylight are the local hours an inverter should answer.
const daylightFrom, daylightTo = 9, 17

func init() {
	Register("ups.on_battery", Cross, nil, upsOnBattery)
	Register("ups.runtime_low", Cross, map[string]any{"minutes": 10.0}, upsRuntimeLow)
	Register("ups.replace_battery", Cross, nil, upsReplaceBattery)
	Register("opendtu.offline", opendtuSvc, nil, on(opendtuOffline))
	Register("cross.charge_expensive", Cross, map[string]any{"factor": 1.3}, chargeExpensive)
}

// upsDevices lists every UPS of every tool with its tool.
func upsDevices(env Env) []upsOf {
	var out []upsOf
	for _, raw := range env.Datasets {
		if d, ok := raw.(*sources.UPSDataset); ok {
			for _, u := range d.Devices {
				out = append(out, upsOf{UPS: u, tool: d.Tool, url: d.URL})
			}
		}
	}
	return out
}

type upsOf struct {
	sources.UPS
	tool enums.ServiceType
	url  string
}

func (u upsOf) finding(msg string, level enums.Severity, params map[string]any) Finding {
	params["name"] = u.Name
	return Finding{Fingerprint: string(u.tool) + ":" + u.Name, Severity: level, Message: msg, Params: params,
		ActionURL: u.url, ActionLabel: "open_in_" + string(u.tool), Sources: []string{string(u.tool)}}
}

func upsOnBattery(_ any, _ map[string]any, env Env) []Finding {
	var found []Finding
	for _, u := range upsDevices(env) {
		if u.OnBattery {
			found = append(found, u.finding("ups.on_battery", enums.SeverityCritical,
				map[string]any{"charge": Num(u.Charge, 0), "minutes": u.Runtime / 60}))
		}
	}
	return found
}

func upsRuntimeLow(_ any, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, u := range upsDevices(env) {
		if u.Runtime > 0 && float64(u.Runtime)/60 < cfgFloat(cfg, "minutes") {
			found = append(found, u.finding("ups.runtime_low", enums.SeverityWarn,
				map[string]any{"minutes": u.Runtime / 60, "load": Num(u.Load, 0)}))
		}
	}
	return found
}

func upsReplaceBattery(_ any, _ map[string]any, env Env) []Finding {
	var found []Finding
	for _, u := range upsDevices(env) {
		if u.ReplaceBattery {
			found = append(found, u.finding("ups.replace_battery", enums.SeverityWarn, map[string]any{}))
		}
	}
	return found
}

// upsOnBatteryName is the first UPS on battery, "" if none.
func upsOnBatteryName(env Env) string {
	for _, u := range upsDevices(env) {
		if u.OnBattery {
			return u.Name
		}
	}
	return ""
}

func opendtuOffline(data *sources.SolarDataset, _ map[string]any, env Env) []Finding {
	hour := env.Today.In(berlinZone()).Hour()
	if hour < daylightFrom || hour >= daylightTo {
		return nil
	}
	var found []Finding
	for _, inv := range data.Inverters {
		if !inv.Reachable {
			found = append(found, svcFinding(opendtuSvc, "opendtu.offline", "offline:"+inv.Serial, "opendtu.offline", enums.SeverityWarn,
				data.URL, map[string]any{"name": inv.Name}))
		}
	}
	return found
}

// berlinZone is the zone daylight is judged in.
func berlinZone() *time.Location {
	if loc, err := time.LoadLocation("Europe/Berlin"); err == nil {
		return loc
	}
	return time.UTC
}

func chargeExpensive(_ any, cfg map[string]any, env Env) []Finding {
	evcc, ok := env.Datasets[string(enums.ServiceEVCC)].(*sources.EVCCDataset)
	tib, ok2 := env.Datasets[string(enums.ServiceTibber)].(*sources.TibberDataset)
	if !ok || !ok2 || evcc.Grid <= 0 || len(tib.Prices) == 0 {
		return nil
	}
	sum := 0.0
	for _, p := range tib.Prices {
		sum += p.Total
	}
	avg := sum / float64(len(tib.Prices))
	if tib.Current < avg*cfgFloat(cfg, "factor") {
		return nil
	}
	var found []Finding
	for _, lp := range evcc.Loadpoints {
		if !lp.Charging || lp.ChargePower <= evcc.PV {
			continue
		}
		found = append(found, Finding{Fingerprint: "expensive:" + lp.Title, Severity: enums.SeverityInfo, Message: "cross.charge_expensive",
			Params:  map[string]any{"point": lp.Title, "price": Num(tib.Current*100, 0), "avg": Num(avg*100, 0), "kw": Num(lp.ChargePower/1000, 1)},
			Sources: []string{string(enums.ServiceEVCC), string(enums.ServiceTibber)}})
	}
	return found
}
