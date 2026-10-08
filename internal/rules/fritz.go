package rules

// The FRITZ!Box beyond its line (fritz.offline, fritz.reconnect: proxies.go):
//
//	fritz.update         FRITZ!OS offers a newer version
//	fritz.update_error   the box's update search or last update failed
//	fritz.mesh_update    repeaters or powerline adapters wait for an update
//	fritz.mesh_weak      a mesh node hangs on a weak WLAN uplink (dBm)
//	fritz.line_margin    a DSL noise margin below "min_db": the line drops
//	fritz.missed_calls   missed calls since yesterday
//	fritz.smart_lost     a Smart Home device lost its DECT link

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	Register("fritz.update", fritzSvc, nil, on(fritzUpdate))
	Register("fritz.update_error", fritzSvc, nil, on(fritzUpdateError))
	Register("fritz.mesh_update", fritzSvc, nil, on(fritzMeshUpdate))
	Register("fritz.mesh_weak", fritzSvc, map[string]any{"min_dbm": -75.0}, on(fritzMeshWeak))
	Register("fritz.line_margin", fritzSvc, map[string]any{"min_db": 6.0}, on(fritzLineMargin))
	Register("fritz.missed_calls", fritzSvc, map[string]any{"days": 1.0}, on(fritzMissed))
	Register("fritz.smart_lost", fritzSvc, nil, on(fritzSmartLost))
	topicRules[TopicUpdates] = append(topicRules[TopicUpdates], "fritz.update", "fritz.mesh_update")
}

func fritzUpdate(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	if data.Update == "" {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.update", "update", "fritz.update", enums.SeverityInfo, data.URL,
		map[string]any{"version": data.Update, "installed": orDash(data.Firmware)})}
}

func fritzUpdateError(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	if !data.UpdateError {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.update_error", "update_error", "fritz.update_error", enums.SeverityWarn, data.URL,
		map[string]any{"installed": orDash(data.Firmware)})}
}

func fritzMeshUpdate(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	names := data.MeshUpdates()
	if len(names) == 0 {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.mesh_update", "mesh_update", "fritz.mesh_update", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(names), "names": shortList(names)})}
}

// fritzMeshWeak: a repeater far from its uplink slows every device on it.
func fritzMeshWeak(data *sources.FritzDataset, cfg map[string]any, _ Env) []Finding {
	limit := cfgFloat(cfg, "min_dbm")
	var found []Finding
	for _, n := range data.Mesh {
		if n.Uplink != sources.FritzWLAN || n.Signal == 0 || float64(n.Signal) >= limit {
			continue
		}
		found = append(found, svcFinding(fritzSvc, "fritz.mesh_weak", "weak:"+n.Name, "fritz.mesh_weak", enums.SeverityInfo, data.URL,
			map[string]any{"name": n.Name, "dbm": n.Signal, "rate": n.Rate}))
	}
	return found
}

// fritzLineMargin: below about 6 dB a DSL line loses sync on noise.
func fritzLineMargin(data *sources.FritzDataset, cfg map[string]any, _ Env) []Finding {
	limit := cfgFloat(cfg, "min_db")
	low := func(m float64) bool { return m > 0 && m < limit }
	if !low(data.DownMargin) && !low(data.UpMargin) {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.line_margin", "margin", "fritz.line_margin", enums.SeverityWarn, data.URL,
		map[string]any{"down": Num(data.DownMargin, 1), "up": Num(data.UpMargin, 1), "min": Num(limit, 0)})}
}

func fritzMissed(data *sources.FritzDataset, cfg map[string]any, env Env) []Finding {
	if data.Calls == nil {
		return nil
	}
	since := env.Today.AddDate(0, 0, -cfgInt(cfg, "days"))
	var recent []sources.FritzCall
	for _, c := range data.Calls.Recent {
		if !c.At.Before(since) {
			recent = append(recent, c)
		}
	}
	if len(recent) == 0 {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.missed_calls", "missed", "fritz.missed_calls", enums.SeverityInfo, data.URL,
		map[string]any{"count": len(recent), "who": orDash(recent[0].Who), "day": Day(recent[0].At)})}
}

func fritzSmartLost(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, d := range data.Smart {
		if d.Present {
			continue
		}
		found = append(found, svcFinding(fritzSvc, "fritz.smart_lost", "smart:"+d.Name, "fritz.smart_lost", enums.SeverityWarn, data.URL,
			map[string]any{"name": d.Name, "product": orDash(d.Product)}))
	}
	return found
}
