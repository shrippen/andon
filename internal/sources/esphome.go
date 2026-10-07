package sources

// ESPHome: the dashboard's nodes with their firmware against the
// dashboard's own version, and which answer. Without a password the
// dashboard needs no login; with one, Basic auth.
//
//	GET devices  → {configured: [{name, friendly_name, configuration, address, target_platform, deployed_version, current_version}]}
//	GET ping     → {"<configuration>.yaml": true | false | null}
//	GET version  → {version}

import (
	"context"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// ESPDevice is one node. Online is nil while the dashboard has not
// pinged it yet.
type ESPDevice struct {
	Name     string // "werkstatt-klima", also the start of its entity ids in Home Assistant
	Friendly string
	Platform string
	Address  string
	Deployed string // firmware on the node, "" = never flashed
	Online   *bool
}

// ESPHomeDataset is the dashboard's version and nodes.
type ESPHomeDataset struct {
	URL     string
	Version string
	Devices []ESPDevice
}

var ESPHomeData = source{key: "esphome.data", ttl: opsTTL, service: enums.ServiceESPHome, fetch: fetchESPHome}

func fetchESPHome(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoESPHome(time.Now().UTC()), nil
	}
	api := basicOrNone(sctx)
	raw, err := api.Get(ctx, "devices", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	pings, _ := api.Get(ctx, "ping", nil) // nodes stay "unknown" without it
	online := asMap(pings)

	data := &ESPHomeDataset{URL: sctx.URL}
	for _, item := range asList(asMap(raw)["configured"]) {
		m := asMap(item)
		d := ESPDevice{Name: asStr(m["name"]), Friendly: firstStr(asStr(m["friendly_name"]), asStr(m["name"])), Platform: strings.ToUpper(asStr(m["target_platform"])),
			Address: asStr(m["address"]), Deployed: asStr(m["deployed_version"])}
		if up, ok := online[asStr(m["configuration"])].(bool); ok {
			d.Online = &up
		}
		data.Version = firstStr(data.Version, asStr(m["current_version"]))
		data.Devices = append(data.Devices, d)
	}
	if data.Version == "" {
		if v, err := api.Get(ctx, "version", nil); err == nil {
			data.Version = asStr(asMap(v)["version"])
		}
	}
	return data, nil
}

func DemoESPHome(now time.Time) *ESPHomeDataset {
	var p struct {
		URL, Version string
		Devices      []struct {
			Name, FriendlyName, Platform, Address, DeployedVersion string
			Online                                                 bool
		}
	}
	demoworld.MustDecode("esphome", now, &p)
	data := &ESPHomeDataset{URL: p.URL, Version: p.Version}
	for _, d := range p.Devices {
		up := d.Online
		data.Devices = append(data.Devices, ESPDevice{Name: d.Name, Friendly: d.FriendlyName, Platform: d.Platform, Address: d.Address, Deployed: d.DeployedVersion, Online: &up})
	}
	return data
}

func init() {
	Register(ESPHomeData)
	Register(testOf{ESPHomeData, func(d any) map[string]any { return map[string]any{"devices": len(d.(*ESPHomeDataset).Devices)} }})
}
