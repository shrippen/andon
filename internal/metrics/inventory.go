package metrics

// Devices in the network against Snipe-IT: the router's leases and the
// FRITZ!Box's active hosts, each name once; one no asset is named like
// is uninventoried (cross.device_uninventoried, services/inventory).
//
//	router leases ─┐
//	FRITZ!Box hosts ┴─► NetClient ─► not in Snipe-IT, not ignored ─► case

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// NetClient is a device the network knows; IP and MAC only from the
// FRITZ!Box.
type NetClient struct {
	Name, IP, MAC string
	From          enums.ServiceType
}

// Key is the client's case key: "d-" and its name made safe.
func (c NetClient) Key() string { return subKey("", "d "+c.Name) }

// NetClients are the devices the router and the FRITZ!Box know, each
// name once, and the services that named them. Of the FRITZ!Box: the
// devices online now, without guests and its own AVM devices.
func NetClients(gw *sources.GatewayDataset, fritz *sources.FritzDataset) (clients []NetClient, from []enums.ServiceType) {
	seen := map[string]bool{}
	add := func(c NetClient) {
		if c.Name != "" && !seen[c.Name] {
			seen[c.Name] = true
			clients = append(clients, c)
		}
	}
	if gw != nil && len(gw.ClientNames) > 0 {
		from = append(from, enums.ServiceGateway)
		for _, n := range gw.ClientNames {
			add(NetClient{Name: n, From: enums.ServiceGateway})
		}
	}
	if fritz != nil && len(fritz.Hosts) > 0 {
		from = append(from, enums.ServiceFritzBox)
		for _, h := range fritz.Hosts {
			if h.Active && !h.Guest && h.Model == "" {
				add(NetClient{Name: h.Name, IP: h.IP, MAC: h.MAC, From: enums.ServiceFritzBox})
			}
		}
	}
	return clients, from
}

// Uninventoried are the clients no Snipe-IT asset is named like ("NAS"
// ~ "nas-keller"), leaving out ignored names (lower case).
func Uninventoried(clients []NetClient, snipe *sources.SnipeDataset, ignore map[string]bool) []NetClient {
	var assets []string
	for _, a := range snipe.Assets {
		if name := strings.ToLower(a.Name); name != "" {
			assets = append(assets, name)
		}
	}
	var out []NetClient
	for _, c := range clients {
		lower := strings.ToLower(c.Name)
		if ignore[lower] {
			continue
		}
		known := false
		for _, a := range assets {
			known = known || strings.Contains(lower, a) || strings.Contains(a, lower)
		}
		if !known {
			out = append(out, c)
		}
	}
	return out
}
