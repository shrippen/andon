package sources

// Network control planes:
//
//	tailscale  Tailscale API or Headscale     devices: online, key expiry, updates
//	gateway    OPNsense, pfSense, UniFi       WAN gateways, devices, firmware updates

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	tailscaleHost   = "api.tailscale.com"
	defaultTailnet  = "-"
	onlineWindow    = 5 * time.Minute
	gatewayOPNsense = "opnsense"
	gatewayPfSense  = "pfsense"
	gatewayUniFi    = "unifi"
	gatewayOpenWrt  = "openwrt"
	unifiPageSize   = "200"
)

// ── Tailscale / Headscale ──

// TailDevice is one device of a tailnet.
type TailDevice struct {
	Name      string
	Online    bool
	LastSeen  time.Time
	KeyExpiry time.Time // zero = never expires
	Update    bool
	Tags      []string // "tag:server"
}

// tailTags merges the tag lists a device or node carries, without doubles.
func tailTags(d map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		for _, raw := range asList(d[k]) {
			if t := asStr(raw); t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

type TailscaleDataset struct {
	URL       string
	Headscale bool
	Devices   []TailDevice
}

type TailscaleData struct{}

func (TailscaleData) Key() string                { return "tailscale.data" }
func (TailscaleData) TTL() time.Duration         { return opsTTL }
func (TailscaleData) Service() enums.ServiceType { return enums.ServiceTailscale }

func (TailscaleData) Fetch(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTailscale(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	now := time.Now().UTC()

	u, _ := url.Parse(sctx.URL)
	if u != nil && strings.EqualFold(u.Hostname(), tailscaleHost) {
		tailnet := asStr(sctx.Options["tailnet"])
		if tailnet == "" {
			tailnet = defaultTailnet
		}
		body, err := api.Get(ctx, "api/v2/tailnet/"+url.PathEscape(tailnet)+"/devices", url.Values{"fields": {"all"}})
		if err != nil {
			return nil, fetchError(err)
		}
		data := &TailscaleDataset{URL: sctx.URL}
		for _, raw := range asList(asMap(body)["devices"]) {
			d := asMap(raw)
			dev := TailDevice{Name: firstStr(asStr(d["hostname"]), asStr(d["name"])), LastSeen: parseTime(d["lastSeen"]), Update: asBool(d["updateAvailable"]),
				Tags: tailTags(d, "tags")}
			dev.Online = asBool(d["connectedToControl"]) || now.Sub(dev.LastSeen) < onlineWindow
			if !asBool(d["keyExpiryDisabled"]) {
				dev.KeyExpiry = parseTime(d["expires"])
			}
			data.Devices = append(data.Devices, dev)
		}
		return data, nil
	}

	body, err := api.Get(ctx, "api/v1/node", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &TailscaleDataset{URL: sctx.URL, Headscale: true}
	for _, raw := range asList(asMap(body)["nodes"]) {
		d := asMap(raw)
		dev := TailDevice{Name: firstStr(asStr(d["givenName"]), asStr(d["name"])), Online: asBool(d["online"]), LastSeen: parseTime(d["lastSeen"]),
			Tags: tailTags(d, "tags", "forcedTags", "validTags")}
		// Headscale reports "0001-01-01T00:00:00Z" for keys that never expire.
		if exp := parseTime(d["expiry"]); exp.Year() > 1 {
			dev.KeyExpiry = exp
		}
		data.Devices = append(data.Devices, dev)
	}
	return data, nil
}

// firstStr returns the first non-empty string.
func firstStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ── Gateways: OPNsense, pfSense, UniFi ──

// GatewayLink is one WAN gateway.
type GatewayLink struct {
	Name          string
	Up            bool
	Loss, DelayMS float64
}

// NetDevice is one managed network device (UniFi).
type NetDevice struct {
	Name           string
	Online, Update bool
}

type GatewayDataset struct {
	URL     string
	Kind    string
	Version string
	Model   string // OpenWrt: the router's model
	Updates int
	Clients int // OpenWrt: devices with a DHCP lease
	// ClientNames are the leased devices' host names (else MAC or IP), sorted.
	ClientNames []string
	Gateways    []GatewayLink
	Devices     []NetDevice
}

type GatewayData struct{}

func (GatewayData) Key() string                { return "gateway.data" }
func (GatewayData) TTL() time.Duration         { return opsTTL }
func (GatewayData) Service() enums.ServiceType { return enums.ServiceGateway }

func (GatewayData) Fetch(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoGateway(), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	kind := asStr(sctx.Options["kind"])
	var data *GatewayDataset
	switch kind {
	case gatewayPfSense:
		data, err = pfSense(ctx, services.HeaderApi(sctx.URL, "X-API-Key", secret, sctx.TLS()))
	case gatewayUniFi:
		data, err = uniFi(ctx, services.HeaderApi(sctx.URL, "X-API-KEY", secret, sctx.TLS()))
	case gatewayOpenWrt:
		data, err = openWrt(ctx, services.UbusApi{URL: sctx.URL, Verify: sctx.VerifyTLS}, secret)
	default:
		kind = gatewayOPNsense
		data, err = opnSense(ctx, services.BasicApi(sctx.URL, secret, sctx.TLS()))
	}
	if err != nil {
		return nil, fetchError(err)
	}
	data.URL, data.Kind = sctx.URL, kind
	return data, nil
}

// leadingFloat reads "12.3 ms" or "0.0 %" as 12.3 / 0.
func leadingFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	field, _, _ := strings.Cut(strings.TrimSpace(asStr(v)), " ")
	f, _ := strconv.ParseFloat(strings.TrimSuffix(field, "%"), 64)
	return f
}

func opnSense(ctx context.Context, api services.KeyedApi) (*GatewayDataset, error) {
	firmware, err := api.Get(ctx, "api/core/firmware/status", nil)
	if err != nil {
		return nil, err
	}
	fw := asMap(firmware)
	data := &GatewayDataset{Version: firstStr(asStr(fw["product_version"]), asStr(asMap(fw["product"])["product_version"]))}
	data.Updates = len(asList(fw["upgrade_packages"])) + len(asList(fw["new_packages"]))
	if asStr(fw["status"]) == "update" && data.Updates == 0 {
		data.Updates = 1
	}

	status, err := api.Get(ctx, "api/routes/gateway/status", nil)
	if err != nil {
		return nil, err
	}
	for _, raw := range asList(asMap(status)["items"]) {
		g := asMap(raw)
		// OPNsense says "none" for a gateway without alarms.
		up := asStr(g["status"]) == "none" || strings.EqualFold(asStr(g["status_translated"]), "online")
		data.Gateways = append(data.Gateways, GatewayLink{Name: asStr(g["name"]), Up: up, Loss: leadingFloat(g["loss"]), DelayMS: leadingFloat(g["delay"])})
	}
	return data, nil
}

func pfSense(ctx context.Context, api services.KeyedApi) (*GatewayDataset, error) {
	gateways, err := api.Get(ctx, "api/v2/status/gateways", nil)
	if err != nil {
		return nil, err
	}
	data := &GatewayDataset{}
	for _, raw := range asList(asMap(gateways)["data"]) {
		g := asMap(raw)
		status := strings.ToLower(asStr(g["status"]))
		data.Gateways = append(data.Gateways, GatewayLink{Name: asStr(g["name"]), Up: status == "online" || status == "none",
			Loss: leadingFloat(g["loss"]), DelayMS: leadingFloat(g["delay"])})
	}
	if version, err := api.Get(ctx, "api/v2/system/version", nil); err == nil {
		data.Version = asStr(asMap(asMap(version)["data"])["version"])
	}
	return data, nil
}

func uniFi(ctx context.Context, api services.KeyedApi) (*GatewayDataset, error) {
	const base = "proxy/network/integration/v1/sites"
	sites, err := api.Get(ctx, base, nil)
	if err != nil {
		return nil, err
	}
	data := &GatewayDataset{}
	for _, raw := range asList(asMap(sites)["data"]) {
		id := asStr(asMap(raw)["id"])
		devices, err := api.Get(ctx, base+"/"+url.PathEscape(id)+"/devices", url.Values{"limit": {unifiPageSize}})
		if err != nil {
			return nil, err
		}
		for _, d := range asList(asMap(devices)["data"]) {
			m := asMap(d)
			dev := NetDevice{Name: asStr(m["name"]), Online: strings.EqualFold(asStr(m["state"]), "online"), Update: asBool(m["firmwareUpdatable"])}
			if dev.Update {
				data.Updates++
			}
			data.Devices = append(data.Devices, dev)
		}
	}
	return data, nil
}

// ubusRoot is the login when the secret names no user.
const ubusRoot = "root"

// openWrt reads an OpenWrt router over rpcd (the login needs read access
// to system, network.interface and, for clients, luci-rpc):
//
//	system board            version, model
//	network.interface dump  WAN links = interfaces with a default route
//	luci-rpc getDHCPLeases  clients (optional, comes with LuCI)
func openWrt(ctx context.Context, api services.UbusApi, secret string) (*GatewayDataset, error) {
	user, password, found := strings.Cut(secret, ":")
	if !found {
		user, password = ubusRoot, secret
	}
	session, err := api.Login(ctx, user, password)
	if err != nil {
		return nil, err
	}
	board, err := api.Call(ctx, session, "system", "board", nil)
	if err != nil {
		return nil, err
	}
	data := &GatewayDataset{Version: asStr(asMap(asMap(board)["release"])["version"]), Model: asStr(asMap(board)["model"])}

	dump, err := api.Call(ctx, session, "network.interface", "dump", nil)
	if err != nil {
		return nil, err
	}
	for _, raw := range asList(asMap(dump)["interface"]) {
		iface := asMap(raw)
		if defaultRoute(iface) {
			data.Gateways = append(data.Gateways, GatewayLink{Name: asStr(iface["interface"]), Up: asBool(iface["up"])})
		}
	}

	if leases, err := api.Call(ctx, session, "luci-rpc", "getDHCPLeases", nil); err == nil {
		seen := map[string]bool{}
		for _, key := range []string{"dhcp_leases", "dhcp6_leases"} {
			for _, raw := range asList(asMap(leases)[key]) {
				l := asMap(raw)
				seen[firstStr(asStr(l["hostname"]), asStr(l["macaddr"]), asStr(l["ipaddr"]))] = true
			}
		}
		delete(seen, "")
		data.Clients = len(seen)
		for name := range seen {
			data.ClientNames = append(data.ClientNames, name)
		}
		sort.Strings(data.ClientNames)
	}
	return data, nil
}

// defaultRoute tells whether an interface carries a default route (IPv4
// 0.0.0.0/0 or IPv6 ::/0), i.e. is an uplink.
func defaultRoute(iface map[string]any) bool {
	for _, raw := range asList(iface["route"]) {
		r := asMap(raw)
		if target := asStr(r["target"]); (target == "0.0.0.0" || target == "::") && asFloat(r["mask"]) == 0 {
			return true
		}
	}
	return false
}

// ── Demo ──

func DemoTailscale(now time.Time) *TailscaleDataset {
	return &TailscaleDataset{URL: "https://api.tailscale.com", Devices: []TailDevice{
		{Name: "nas", Online: true, LastSeen: now, Tags: []string{"tag:server"}},
		{Name: "laptop", Online: true, LastSeen: now, KeyExpiry: now.AddDate(0, 0, 9)},
		{Name: "pi", LastSeen: now.AddDate(0, 0, -12), Update: true, Tags: []string{"tag:server", "tag:iot"}},
	}}
}

func DemoGateway() *GatewayDataset {
	return &GatewayDataset{URL: "https://opnsense.demo", Kind: gatewayOPNsense, Version: "25.7.3", Updates: 2,
		Gateways: []GatewayLink{{Name: "WAN_DHCP", Up: true, DelayMS: 11.4}, {Name: "LTE", Up: false, Loss: 100}}}
}

func init() {
	Register(TailscaleData{})
	Register(testOf{TailscaleData{}, func(d any) map[string]any { return map[string]any{"devices": len(d.(*TailscaleDataset).Devices)} }})
	Register(GatewayData{})
	Register(testOf{GatewayData{}, func(d any) map[string]any { return map[string]any{"version": d.(*GatewayDataset).Version} }})
}
