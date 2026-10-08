package sources

// FRITZ!Box over TR-064 (services.TR064): the internet connection, the
// line, FRITZ!OS, throughput, the devices and mesh, calls and Smart Home.
// A FRITZ!Box user with the right "FRITZ!Box settings" is needed (calls:
// also "voice messages, fax, FRITZ!App Fon and call list"; Smart Home:
// "Smart Home"); the secret is "user:password". Everything after the
// connection is optional: a box or user without it leaves its part empty.
//
//	wanpppconn1 / wanipconnection1  GetInfo → NewConnectionStatus, NewUptime, NewLastConnectionError
//	wandslifconfig1                 GetInfo → …CurrRate, …MaxRate (kbit/s), …NoiseMargin (0.1 dB)
//	deviceinfo                      GetInfo → NewModelName, NewSoftwareVersion "154.08.03"
//	userif                          GetInfo → NewUpgradeAvailable, NewX_AVM-DE_Version, NewX_AVM-DE_UpdateState
//	wancommonifconfig1              X_AVM-DE_GetOnlineMonitor → Newds_current_bps "newest,…" (bytes/s, 5 s apart)
//	wlanconfig1…3                   GetInfo, GetTotalAssociations → band, state, channel, clients
//	hosts                           X_AVM-DE_GetHostListPath, X_AVM-DE_GetMeshListPath → lists (fritzlists.go)
//	x_contact                       GetCallList → NewCallListURL (fritzlists.go)
//	x_homeauto                      GetGenericDeviceInfos(NewIndex) until UPnP 713 (fritzlists.go)

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	// fritzConnected is TR-064's word for a working internet connection.
	fritzConnected = "Connected"
	// fritzUpdateError is the update state after a failed search or update.
	fritzUpdateError = "Error"
	// fritzUnnamed stands for an update the box offers without its version.
	fritzUnnamed = "?"
	// fritzTenths: TR-064 gives noise margins in tenths of a dB.
	fritzTenths = 10.0
	// bytesToKbit turns the online monitor's bytes/s into kbit/s.
	bytesToKbit = 8.0 / 1000
)

// FritzDataset is the box: its line, FRITZ!OS and what hangs on it.
type FritzDataset struct {
	URL       string
	Model     string
	Status    string    // Connected, Connecting, Disconnected …
	Uptime    int       // seconds since the connection came up
	Since     time.Time // when the connection came up, to the minute; zero while down
	LastError string
	// Sync and max rates of a DSL line in kbit/s; 0 on cable and fibre.
	DownSync, UpSync, DownMax, UpMax int
	// Noise margins of a DSL line in dB; 0 on cable and fibre.
	DownMargin, UpMargin float64

	// FRITZ!OS: the running version ("8.03"), the offered one ("" = none),
	// and whether the box's last update search or update failed.
	Firmware, Update string
	UpdateError      bool

	// Throughput of the last 100 s in kbit/s, one value per 5 s, oldest
	// first; nil when the box has no online monitor.
	TrafficDown, TrafficUp []int

	Hosts  []FritzHost  // devices the box knows, active or not
	Mesh   []FritzNode  // AVM devices: the box, repeaters, powerline
	Radios []FritzRadio // the box's own WLAN radios
	Calls  *FritzCalls  // nil = no call list
	Smart  []FritzSmart // Smart Home devices (DECT)
}

// Connected reports whether the internet connection is up.
func (d *FritzDataset) Connected() bool { return d.Status == fritzConnected }

// Active counts the devices online now.
func (d *FritzDataset) Active() int {
	n := 0
	for _, h := range d.Hosts {
		if h.Active {
			n++
		}
	}
	return n
}

// MeshUpdates are the AVM devices besides the box with an update waiting.
func (d *FritzDataset) MeshUpdates() []string {
	var out []string
	for _, h := range d.Hosts {
		if h.Update {
			out = append(out, h.Name)
		}
	}
	return out
}

// FritzLink is how a device or mesh node hangs on the network.
type FritzLink string

const (
	FritzNoLink FritzLink = ""
	FritzLAN    FritzLink = "lan"
	FritzWLAN   FritzLink = "wlan"
	FritzPLC    FritzLink = "plc" // powerline
)

// FritzHost is one device of the box's network.
type FritzHost struct {
	Name, IP, MAC string
	Active        bool
	Link          FritzLink
	Speed         int // Mbit/s of its link, 0 = unknown
	Signal        int // WLAN signal in dBm, 0 = unknown
	Guest         bool
	Model         string // AVM devices: "FRITZ!Repeater 1200 AX"
	Update        bool   // an AVM device with an update waiting
}

// FritzNode is one AVM device of the mesh. Uplink and Rate are its way
// towards the box (none for the box itself).
type FritzNode struct {
	Name, Model, Firmware string
	Role                  string // master, slave; "" outside the mesh
	Uplink                FritzLink
	Rate                  int // Mbit/s of the uplink, 0 = unknown
	Signal                int // dBm of a WLAN uplink, 0 = unknown
	Clients               int // devices linked to it, other mesh nodes left out
}

// FritzRadio is one WLAN band of the box.
type FritzRadio struct {
	Band    string // "2.4", "5", "6" (GHz)
	On      bool
	Channel int
	Clients int
}

// FritzCalls is the call list of the last days, counted.
type FritzCalls struct {
	Days            int
	In, Out, Missed int
	Recent          []FritzCall // missed calls, newest first
}

// FritzCall is a missed call: when, and who (name, else number, else "").
type FritzCall struct {
	At  time.Time
	Who string
}

// FritzSmart is one Smart Home device; each part is set when it has it.
type FritzSmart struct {
	Name, Product string
	Present       bool
	Switch        string  // on, off; "" = no switch
	Meter         bool    // Power and Energy are measured
	Power         float64 // W
	Energy        float64 // kWh since it was set up
	Thermo        bool    // Temp is measured
	Temp          float64 // °C
	Heater        bool    // a radiator thermostat: Set is its target
	Set           float64 // °C
}

var FritzData = source{key: "fritzbox.data", ttl: opsTTL, service: enums.ServiceFritzBox, fetch: fetchFritz}

func fetchFritz(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoFritz(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, pass, _ := strings.Cut(secret, ":")
	box := services.TR064{URL: sctx.URL, User: user, Password: pass, Mode: sctx.TLS()}

	// PPP lines (DSL) and IP connections (cable, fibre, LTE) answer on
	// different services.
	conn, err := box.Call(ctx, "wanpppconn1", "WANPPPConnection:1", "GetInfo")
	if err != nil || conn["NewConnectionStatus"] == "" {
		if conn, err = box.Call(ctx, "wanipconnection1", "WANIPConnection:1", "GetInfo"); err != nil {
			return nil, fetchError(err)
		}
	}
	data := &FritzDataset{URL: sctx.URL, Status: conn["NewConnectionStatus"], Uptime: atoiOr(conn["NewUptime"]), LastError: conn["NewLastConnectionError"]}
	if data.Connected() && data.Uptime > 0 {
		// To the minute: runs a few seconds apart name the same connection.
		data.Since = time.Now().UTC().Add(-time.Duration(data.Uptime) * time.Second).Round(time.Minute)
	}
	if dsl, err := box.Call(ctx, "wandslifconfig1", "WANDSLInterfaceConfig:1", "GetInfo"); err == nil {
		data.DownSync, data.UpSync = atoiOr(dsl["NewDownstreamCurrRate"]), atoiOr(dsl["NewUpstreamCurrRate"])
		data.DownMax, data.UpMax = atoiOr(dsl["NewDownstreamMaxRate"]), atoiOr(dsl["NewUpstreamMaxRate"])
		data.DownMargin = float64(atoiOr(dsl["NewDownstreamNoiseMargin"])) / fritzTenths
		data.UpMargin = float64(atoiOr(dsl["NewUpstreamNoiseMargin"])) / fritzTenths
	}
	if info, err := box.Call(ctx, "deviceinfo", "DeviceInfo:1", "GetInfo"); err == nil {
		data.Model, data.Firmware = info["NewModelName"], fritzOS(info["NewSoftwareVersion"])
	}
	if ui, err := box.Call(ctx, "userif", "UserInterface:1", "GetInfo"); err == nil {
		data.Update, data.UpdateError = fritzUpdate(ui)
	}

	data.TrafficDown, data.TrafficUp = fritzTraffic(ctx, box)
	data.Radios = fritzRadios(ctx, box)
	data.Hosts, data.Mesh = fritzNetwork(ctx, box)
	data.Calls = fritzCalls(ctx, box, time.Now())
	data.Smart = fritzSmart(ctx, box)
	return data, nil
}

func atoiOr(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// fritzOS shortens a FRITZ!OS version to what AVM names: "154.08.03" →
// "8.03" (the first part is the model); other versions stay as they are
// ("3.0.0.0-38").
func fritzOS(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return v
	}
	return strings.TrimPrefix(parts[1], "0") + "." + parts[2]
}

// fritzUpdate reads the offered version and the update state.
func fritzUpdate(ui map[string]string) (offered string, failed bool) {
	failed = ui["NewX_AVM-DE_UpdateState"] == fritzUpdateError
	if ui["NewUpgradeAvailable"] != "1" {
		return "", failed
	}
	if v := ui["NewX_AVM-DE_Version"]; v != "" {
		return fritzOS(v), failed
	}
	return fritzUnnamed, failed
}

// fritzTraffic reads the online monitor: "newest,…" bytes/s → kbit/s,
// oldest first.
func fritzTraffic(ctx context.Context, box services.TR064) (down, up []int) {
	mon, err := box.Call(ctx, "wancommonifconfig1", "WANCommonInterfaceConfig:1", "X_AVM-DE_GetOnlineMonitor", services.Arg{Name: "NewSyncGroupIndex", Value: "0"})
	if err != nil {
		return nil, nil
	}
	series := func(raw string) []int {
		var out []int
		for _, f := range strings.Split(raw, ",") {
			if f = strings.TrimSpace(f); f != "" {
				out = append(out, int(float64(atoiOr(f))*bytesToKbit))
			}
		}
		slices.Reverse(out)
		return out
	}
	return series(mon["Newds_current_bps"]), series(mon["Newus_current_bps"])
}

// fritzBands names the radios' frequency bands (MHz) in GHz; a radio
// without one (the guest network's) is left out.
var fritzBands = map[string]string{"2400": "2.4", "5000": "5", "6000": "6"}

// fritzRadioCount is how many WLAN services a box has at most.
const fritzRadioCount = 4

func fritzRadios(ctx context.Context, box services.TR064) []FritzRadio {
	var out []FritzRadio
	for i := 1; i <= fritzRadioCount; i++ {
		n := strconv.Itoa(i)
		info, err := box.Call(ctx, "wlanconfig"+n, "WLANConfiguration:"+n, "GetInfo")
		if err != nil {
			break
		}
		band, ok := fritzBands[info["NewX_AVM-DE_FrequencyBand"]]
		if !ok {
			continue
		}
		r := FritzRadio{Band: band, On: info["NewStatus"] == "Up", Channel: atoiOr(info["NewChannel"])}
		if assoc, err := box.Call(ctx, "wlanconfig"+n, "WLANConfiguration:"+n, "GetTotalAssociations"); err == nil {
			r.Clients = atoiOr(assoc["NewTotalAssociations"])
		}
		out = append(out, r)
	}
	return out
}

// DemoFritz is the studio's box; its line came up at the world's daily
// reconnect ("since": a time of today, else of yesterday).
func DemoFritz(now time.Time) *FritzDataset {
	data := &FritzDataset{}
	demoworld.MustDecode("fritzbox", now, data)
	if data.Since.IsZero() {
		return data // a world without the daily reconnect
	}
	if data.Since.After(now) {
		data.Since = data.Since.AddDate(0, 0, -1)
	}
	data.Uptime = int(now.Sub(data.Since).Seconds())
	return data
}

// FritzSnapshot is the box as a run saw it at a time.
type FritzSnapshot struct {
	At   time.Time
	Data *FritzDataset
}

// DemoFritzPast is what earlier runs saw, oldest first: the world's
// reconnects of the last weeks, the line down where a run caught it, and
// last today's connection. The demo seed records them, so the ISP report
// has a history.
func DemoFritzPast(now time.Time) []FritzSnapshot {
	var p struct {
		Reconnects []struct{ Down, Up time.Time }
	}
	demoworld.MustDecode("fritzbox", now, &p)
	var out []FritzSnapshot
	for _, r := range p.Reconnects {
		if !r.Down.IsZero() {
			out = append(out, FritzSnapshot{At: r.Down, Data: &FritzDataset{Status: "Disconnected"}})
		}
		up := r.Up.Round(time.Minute)
		out = append(out, FritzSnapshot{At: up.Add(time.Minute), Data: &FritzDataset{Status: fritzConnected, Since: up}})
	}
	live := DemoFritz(now)
	return append(out, FritzSnapshot{At: live.Since.Add(time.Minute), Data: live})
}

// ── Technitium DNS ──

// GET api/dashboard/stats/get?token=…&type=LastDay → {status, response: {stats: {totalQueries, totalBlocked, totalClients}, topBlockedDomains: [{name, hits}]}}

var TechnitiumData = source{key: "technitium.data", ttl: opsTTL, service: enums.ServiceTechnitium, fetch: fetchTechnitium}

func fetchTechnitium(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTechnitium(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.KeyedApi{URL: sctx.URL, Headers: map[string]string{"Accept": "application/json"}, Verify: sctx.VerifyTLS}
	raw, err := api.Get(ctx, "api/dashboard/stats/get", map[string][]string{"token": {secret}, "type": {"LastDay"}})
	if err != nil {
		return nil, fetchError(err)
	}
	answer := asMap(raw)
	if asStr(answer["status"]) != "ok" {
		return nil, newSourceError("technitium.refused")
	}
	resp := asMap(answer["response"])
	stats := asMap(resp["stats"])
	data := &DNSFilterDataset{URL: sctx.URL, Queries: int(asFloat(stats["totalQueries"])), Blocked: int(asFloat(stats["totalBlocked"])),
		Clients: int(asFloat(stats["totalClients"])), Enabled: true}
	if data.Queries > 0 {
		data.Percent = float64(data.Blocked) / float64(data.Queries) * 100
	}
	for _, raw := range asList(resp["topBlockedDomains"]) {
		d := asMap(raw)
		data.TopBlocked = append(data.TopBlocked, DNSDomain{Domain: asStr(d["name"]), Count: int(asFloat(d["hits"]))})
	}
	return data, nil
}

func DemoTechnitium(now time.Time) *DNSFilterDataset {
	var p struct {
		DNSFilterDataset
		TopBlocked []struct {
			Name  string
			Count int
		}
	}
	demoworld.MustDecode("dns_technitium", now, &p)
	data := &p.DNSFilterDataset
	data.TopBlocked = nil
	for _, d := range p.TopBlocked {
		data.TopBlocked = append(data.TopBlocked, DNSDomain{Domain: d.Name, Count: d.Count})
	}
	if data.Queries > 0 {
		data.Percent = float64(data.Blocked) / float64(data.Queries) * 100
	}
	return data
}

func init() {
	Register(FritzData)
	Register(testOf{FritzData, func(d any) map[string]any { return map[string]any{"status": d.(*FritzDataset).Status} }})
	Register(TechnitiumData)
	Register(testOf{TechnitiumData, func(d any) map[string]any { return map[string]any{"queries": d.(*DNSFilterDataset).Queries} }})
}
