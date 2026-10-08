package rules

// Routes, the line and the devices on the network:
//
//	routes.down                 a proxy's target does not answer; with Docker
//	                            data: its container stopped or missing (cross)
//	routes.cert_expiry          a route's certificate ends within "days" (NPM)
//	cross.route_undocumented    hosts a proxy serves that no IT note names
//	fritz.offline               the FRITZ!Box has no internet connection
//	fritz.reconnect             it reconnected within the last hour
//	cross.line_vs_speed         speed test against the line's sync rate: far
//	                            below → the home network; sync below booked → the line
//	cross.device_uninventoried  clients of the network (router, FRITZ!Box) no
//	                            Snipe-IT asset names

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

var fritzSvc = string(enums.ServiceFritzBox)

const (
	reconnectWindow = time.Hour
	kbitPerMbit     = 1000.0
)

func init() {
	Register("routes.down", Cross, nil, routesDown)
	Register("routes.cert_expiry", Cross, map[string]any{"days": 14.0}, routesCertExpiry)
	Register("cross.route_undocumented", Cross, nil, routeUndocumented)
	Register("fritz.offline", fritzSvc, nil, on(fritzOffline))
	Register("fritz.reconnect", fritzSvc, nil, on(fritzReconnect))
	Register("cross.line_vs_speed", Cross, map[string]any{"share": 0.6, "booked_share": 0.8}, lineVsSpeed)
	Register("cross.device_uninventoried", Cross, map[string]any{"ignore": []any{}}, deviceUninventoried)
}

// proxyRoutes lists every proxy's routes with its tool and URL.
func proxyRoutes(env Env) []routeOf {
	var out []routeOf
	for _, raw := range env.Datasets {
		if d, ok := raw.(*sources.RoutesDataset); ok {
			for _, r := range d.Routes {
				out = append(out, routeOf{ProxyRoute: r, tool: d.Tool, url: d.URL})
			}
		}
	}
	return out
}

type routeOf struct {
	sources.ProxyRoute
	tool enums.ServiceType
	url  string
}

func (r routeOf) finding(msg string, level enums.Severity, params map[string]any) Finding {
	params["host"] = r.Host
	return Finding{Fingerprint: string(r.tool) + ":" + r.Host, Severity: level, Message: msg, Params: params,
		ActionURL: r.url, ActionLabel: "open_in_" + string(r.tool), Sources: []string{string(r.tool)}}
}

// containerStates maps Docker container names (lower case) to running.
func containerStates(env Env) (map[string]bool, bool) {
	d, ok := env.Datasets[string(enums.ServiceDocker)].(*sources.DockerDataset)
	if !ok {
		return nil, false
	}
	out := map[string]bool{}
	for _, c := range d.Containers {
		out[strings.ToLower(c.Name)] = c.State == "running"
	}
	return out, true
}

func routesDown(_ any, _ map[string]any, env Env) []Finding {
	containers, known := containerStates(env)
	var found []Finding
	for _, r := range proxyRoutes(env) {
		if r.Up {
			continue
		}
		msg := "routes.down"
		if known && r.Service != "" {
			running, exists := containers[strings.ToLower(r.Service)]
			switch {
			case !exists:
				msg = "routes.container_missing"
			case !running:
				msg = "routes.container_stopped"
			}
		}
		found = append(found, r.finding(msg, enums.SeverityWarn, map[string]any{"service": r.Service, "target": orDash(r.Target)}))
	}
	return found
}

func routesCertExpiry(_ any, cfg map[string]any, env Env) []Finding {
	limit := env.Today.AddDate(0, 0, int(cfgFloat(cfg, "days")))
	var found []Finding
	for _, r := range proxyRoutes(env) {
		if r.CertExpiry.IsZero() || r.CertExpiry.After(limit) {
			continue
		}
		found = append(found, r.finding("routes.cert_expiry", enums.SeverityWarn, map[string]any{"day": Day(r.CertExpiry)}))
	}
	return found
}

func routeUndocumented(_ any, _ map[string]any, env Env) []Finding {
	gt, ok := env.Datasets[string(enums.ServiceGitea)].(*sources.GiteaDataset)
	if !ok || !gt.NotesRead {
		return nil
	}
	var documented []string
	for _, n := range gt.Notes {
		if n.Web != "" && !n.Deprecated {
			documented = append(documented, strings.ToLower(n.Web))
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, r := range proxyRoutes(env) {
		host := strings.ToLower(r.Host)
		if seen[host] {
			continue
		}
		seen[host] = true
		named := false
		for _, web := range documented {
			named = named || strings.Contains(web, host)
		}
		if !named {
			missing = append(missing, r.Host)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{Fingerprint: "undocumented", Severity: enums.SeverityInfo, Message: "cross.route_undocumented",
		Params: map[string]any{"count": len(missing), "hosts": shortList(missing)}, Sources: []string{string(enums.ServiceGitea)}}}
}

func fritzOffline(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	if data.Connected() {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.offline", "offline", "fritz.offline", enums.SeverityCritical, data.URL,
		map[string]any{"status": orDash(data.Status), "error": orDash(data.LastError)})}
}

func fritzReconnect(data *sources.FritzDataset, _ map[string]any, _ Env) []Finding {
	if !data.Connected() || data.Uptime <= 0 || time.Duration(data.Uptime)*time.Second > reconnectWindow {
		return nil
	}
	return []Finding{svcFinding(fritzSvc, "fritz.reconnect", "reconnect", "fritz.reconnect", enums.SeverityInfo, data.URL,
		map[string]any{"minutes": data.Uptime / 60, "error": orDash(data.LastError)})}
}

func lineVsSpeed(_ any, cfg map[string]any, env Env) []Finding {
	fritz, ok := env.Datasets[fritzSvc].(*sources.FritzDataset)
	speed, ok2 := env.Datasets[string(enums.ServiceSpeedtest)].(*sources.SpeedtestDataset)
	if !ok || !ok2 || fritz.DownSync == 0 || speed.Down == 0 {
		return nil
	}
	sync := float64(fritz.DownSync) / kbitPerMbit
	params := map[string]any{"sync": Num(sync, 0), "down": Num(speed.Down, 0), "booked": Num(speed.ExpectDown, 0)}
	src := []string{fritzSvc, string(enums.ServiceSpeedtest)}
	if speed.ExpectDown > 0 && sync < speed.ExpectDown*cfgFloat(cfg, "booked_share") {
		return []Finding{{Fingerprint: "sync", Severity: enums.SeverityWarn, Message: "cross.sync_below_booked", Params: params, Sources: src}}
	}
	if speed.Down < sync*cfgFloat(cfg, "share") {
		return []Finding{{Fingerprint: "speed", Severity: enums.SeverityInfo, Message: "cross.speed_below_sync", Params: params, Sources: src}}
	}
	return nil
}

func deviceUninventoried(_ any, cfg map[string]any, env Env) []Finding {
	snipe, ok := env.Datasets[string(enums.ServiceSnipeIT)].(*sources.SnipeDataset)
	names, from := networkClients(env)
	if !ok || len(names) == 0 {
		return nil
	}
	ignore := idSet(cfg["ignore"])
	var assets []string
	for _, a := range snipe.Assets {
		assets = append(assets, strings.ToLower(a.Name))
	}
	var missing []string
	for _, name := range names {
		lower := strings.ToLower(name)
		if ignore[lower] {
			continue
		}
		known := false
		for _, a := range assets {
			known = known || (a != "" && (strings.Contains(lower, a) || strings.Contains(a, lower)))
		}
		if !known {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{Fingerprint: "uninventoried", Severity: enums.SeverityInfo, Message: "cross.device_uninventoried",
		Params: map[string]any{"count": len(missing), "names": shortList(missing)}, Sources: append([]string{string(enums.ServiceSnipeIT)}, from...)}}
}

// networkClients are the devices the router and the FRITZ!Box know, each
// name once, and the services that named them. Of the FRITZ!Box: the
// devices online now, without guests and its own AVM devices.
func networkClients(env Env) (names, from []string) {
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if gw, ok := env.Datasets[string(enums.ServiceGateway)].(*sources.GatewayDataset); ok && len(gw.ClientNames) > 0 {
		from = append(from, string(enums.ServiceGateway))
		for _, n := range gw.ClientNames {
			add(n)
		}
	}
	if fritz, ok := env.Datasets[fritzSvc].(*sources.FritzDataset); ok && len(fritz.Hosts) > 0 {
		from = append(from, fritzSvc)
		for _, h := range fritz.Hosts {
			if h.Active && !h.Guest && h.Model == "" {
				add(h.Name)
			}
		}
	}
	return names, from
}
