package sources

// Power at home: UPS (PeaNUT for NUT, apcupsd), solar (OpenDTU) and the
// wallbox (EVCC).
//
//	PeaNUT    GET api/v1/devices?meta=true → [{"ups.status": "OL", "battery.charge": 100, "battery.runtime": 1260, "ups.load": 38, …}]
//	apcupsd   NIS on TCP 3551 (services.ApcupsdStatus): STATUS, BCHARGE, TIMELEFT, LOADPCT …
//	OpenDTU   GET api/livedata/status → {total{Power,YieldDay,YieldTotal{v}}, inverters[{name,serial,reachable,producing,AC{0{Power{v}}}}]}
//	EVCC      GET api/state → {pvPower, homePower, grid{power}, battery{soc}, tariffGrid, loadpoints[…], statistics{30d{…}}}
//	          GET api/sessions → [{created, finished, loadpoint, vehicle, chargedEnergy, price, pricePerKWh}]

import (
	"context"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// UPS is one uninterruptible power supply.
type UPS struct {
	Name, Model    string
	Status         string // the tool's own words: "OL", "OB LB", "ONLINE", "ONBATT"
	OnBattery      bool
	LowBattery     bool
	ReplaceBattery bool
	Charge         float64 // %
	Runtime        int     // seconds at the current load
	Load           float64 // %
	Power          float64 // W, 0 unknown
}

// UPSDataset is the UPS one tool watches.
type UPSDataset struct {
	URL     string
	Tool    enums.ServiceType
	Devices []UPS
}

// SolarInverter is one inverter of OpenDTU.
type SolarInverter struct {
	Name, Serial         string
	Reachable, Producing bool
	Power                float64 // W
}

// SolarDataset is OpenDTU's totals and inverters.
type SolarDataset struct {
	URL        string
	Power      float64 // W now
	YieldDay   float64 // Wh today
	YieldTotal float64 // kWh
	Inverters  []SolarInverter
}

// Loadpoint is one EVCC charging point.
type Loadpoint struct {
	Title, Vehicle      string
	Connected, Charging bool
	ChargePower         float64 // W
	VehicleSoc          float64 // %
	Charged             float64 // kWh this session
	Mode                string  // off, now, minpv, pv
}

// EVCCSession is one finished charging session.
type EVCCSession struct {
	Loadpoint, Vehicle string
	Created, Finished  time.Time
	KWh                float64
	Price              float64 // cost of the session, 0 unknown
}

// evccSessionDays is how far back sessions are kept: last month and this.
const evccSessionDays = 62

// EVCCDataset is EVCC's site now and its last 30 days.
type EVCCDataset struct {
	URL            string
	PV, Home, Grid float64 // W; Grid > 0 = import
	BatterySoc     float64 // %, -1 = no battery
	TariffGrid     float64 // price per kWh now, 0 unknown
	Loadpoints     []Loadpoint
	ChargedKWh30   float64
	AvgPrice30     float64
	SolarPercent30 float64
	Sessions       []EVCCSession // newest first, the last evccSessionDays
}

var (
	PeaNUTData  = source{key: "peanut.data", ttl: opsTTL, service: enums.ServicePeaNUT, fetch: fetchPeaNUT}
	ApcupsdData = source{key: "apcupsd.data", ttl: opsTTL, service: enums.ServiceApcupsd, fetch: fetchApcupsd}
	OpenDTUData = source{key: "opendtu.data", ttl: opsTTL, service: enums.ServiceOpenDTU, fetch: fetchOpenDTU}
	EVCCData    = source{key: "evcc.data", ttl: opsTTL, service: enums.ServiceEVCC, fetch: fetchEVCC}
)

// ── UPS ──

func fetchPeaNUT(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoUPS(time.Now().UTC(), enums.ServicePeaNUT), nil
	}
	raw, err := basicOrNone(sctx).Get(ctx, "api/v1/devices", url.Values{"meta": {"true"}})
	if err != nil {
		return nil, fetchError(err)
	}
	data := &UPSDataset{URL: sctx.URL, Tool: enums.ServicePeaNUT}
	for _, item := range asList(raw) {
		v := asMap(item)
		name := asStr(v["peanut.device_id"])
		if name == "" {
			name = asStr(v["device.model"])
		}
		data.Devices = append(data.Devices, nutUPS(name, asStr(v["device.model"]), asStr(v["ups.status"]), asFloat(v["battery.charge"]),
			int(asFloat(v["battery.runtime"])), asFloat(v["ups.load"]), asFloat(v["ups.realpower"])))
	}
	return data, nil
}

// nutUPS reads NUT's status flags: OL online, OB on battery, LB low
// battery, RB replace battery.
func nutUPS(name, model, status string, charge float64, runtime int, load, power float64) UPS {
	flags := strings.Fields(status)
	has := func(f string) bool {
		for _, x := range flags {
			if x == f {
				return true
			}
		}
		return false
	}
	return UPS{Name: name, Model: model, Status: status, OnBattery: has("OB"), LowBattery: has("LB"), ReplaceBattery: has("RB"),
		Charge: charge, Runtime: runtime, Load: load, Power: power}
}

// apcDefaultPort is the NIS port.
const apcDefaultPort = "3551"

func fetchApcupsd(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoUPS(time.Now().UTC(), enums.ServiceApcupsd), nil
	}
	addr := sctx.URL
	if u, err := url.Parse(sctx.URL); err == nil && u.Host != "" {
		addr = u.Host
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, apcDefaultPort)
	}
	st, err := services.ApcupsdStatus(ctx, addr)
	if err != nil {
		return nil, fetchError(err)
	}
	status := st["STATUS"]
	ups := UPS{Name: st["UPSNAME"], Model: st["MODEL"], Status: status, OnBattery: strings.Contains(status, "ONBATT"),
		LowBattery: strings.Contains(status, "LOWBATT"), ReplaceBattery: strings.Contains(status, "REPLACEBATT"),
		Charge: leadingNumber(st["BCHARGE"]), Runtime: int(leadingNumber(st["TIMELEFT"]) * 60), Load: leadingNumber(st["LOADPCT"])}
	if watts := leadingNumber(st["NOMPOWER"]); watts > 0 {
		ups.Power = watts * ups.Load / 100
	}
	return &UPSDataset{URL: sctx.URL, Tool: enums.ServiceApcupsd, Devices: []UPS{ups}}, nil
}

// leadingNumber reads "92.0 Percent" as 92.
func leadingNumber(s string) float64 {
	f, _ := strconv.ParseFloat(strings.Fields(s + " 0")[0], 64)
	return f
}

// ── Solar ──

func fetchOpenDTU(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoSolar(time.Now().UTC()), nil
	}
	raw, err := basicOrNone(sctx).Get(ctx, "api/livedata/status", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	m := asMap(raw)
	total := asMap(m["total"])
	value := func(o map[string]any, k string) float64 { return asFloat(asMap(o[k])["v"]) }
	data := &SolarDataset{URL: sctx.URL, Power: value(total, "Power"), YieldDay: value(total, "YieldDay"), YieldTotal: value(total, "YieldTotal")}
	for _, item := range asList(m["inverters"]) {
		inv := asMap(item)
		reachable, _ := inv["reachable"].(bool)
		producing, _ := inv["producing"].(bool)
		data.Inverters = append(data.Inverters, SolarInverter{Name: asStr(inv["name"]), Serial: asStr(inv["serial"]), Reachable: reachable,
			Producing: producing, Power: value(asMap(asMap(inv["AC"])["0"]), "Power")})
	}
	return data, nil
}

// ── EVCC ──

func fetchEVCC(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoEVCC(time.Now().UTC()), nil
	}
	raw, err := basicOrNone(sctx).Get(ctx, "api/state", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	m := asMap(raw)
	if inner, wrapped := m["result"].(map[string]any); wrapped {
		m = inner // EVCC before 0.200
	}
	data := &EVCCDataset{URL: sctx.URL, PV: asFloat(m["pvPower"]), Home: asFloat(m["homePower"]), Grid: asFloat(asMap(m["grid"])["power"]),
		BatterySoc: -1, TariffGrid: asFloat(m["tariffGrid"])}
	if g, ok := m["gridPower"]; ok {
		data.Grid = asFloat(g)
	}
	if b := asMap(m["battery"]); b["soc"] != nil {
		data.BatterySoc = asFloat(b["soc"])
	} else if s, ok := m["batterySoc"]; ok {
		data.BatterySoc = asFloat(s)
	}
	for _, item := range asList(m["loadpoints"]) {
		lp := asMap(item)
		connected, _ := lp["connected"].(bool)
		charging, _ := lp["charging"].(bool)
		data.Loadpoints = append(data.Loadpoints, Loadpoint{Title: asStr(lp["title"]), Vehicle: asStr(lp["vehicleTitle"]), Connected: connected,
			Charging: charging, ChargePower: asFloat(lp["chargePower"]), VehicleSoc: asFloat(lp["vehicleSoc"]),
			Charged: asFloat(lp["chargedEnergy"]) / 1000, Mode: asStr(lp["mode"])})
	}
	month := asMap(asMap(m["statistics"])["30d"])
	data.ChargedKWh30, data.AvgPrice30, data.SolarPercent30 = asFloat(month["chargedKWh"]), asFloat(month["avgPrice"]), asFloat(month["solarPercentage"])

	// Sessions are an extra: an EVCC without them still has its state.
	if raw, err := basicOrNone(sctx).Get(ctx, "api/sessions", nil); err == nil {
		data.Sessions = evccSessions(raw, time.Now().UTC())
	}
	return data, nil
}

// evccSessions reads the sessions of the last evccSessionDays, newest
// first; a session without a price costs its energy × price per kWh.
func evccSessions(raw any, now time.Time) []EVCCSession {
	list := asList(raw)
	if inner, wrapped := raw.(map[string]any); wrapped {
		list = asList(inner["result"]) // EVCC before 0.200
	}
	since := now.AddDate(0, 0, -evccSessionDays)
	var out []EVCCSession
	for _, item := range list {
		m := asMap(item)
		s := EVCCSession{Loadpoint: asStr(m["loadpoint"]), Vehicle: asStr(m["vehicle"]), Created: parseTime(m["created"]),
			Finished: parseTime(m["finished"]), KWh: asFloat(m["chargedEnergy"]), Price: asFloat(m["price"])}
		if s.Finished.IsZero() || s.Finished.Before(since) {
			continue
		}
		if s.Price == 0 {
			s.Price = math.Round(s.KWh*asFloat(m["pricePerKWh"])*centsPerUnit) / centsPerUnit
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Finished.After(out[j].Finished) })
	return out
}

// ── Demo ──

// DemoUPS is the studio's UPS of one tool.
func DemoUPS(now time.Time, tool enums.ServiceType) *UPSDataset {
	var p struct {
		URL     string
		Devices []struct {
			Name, Model, Status string
			Charge, Load, Power float64
			Runtime             int
		}
	}
	demoworld.MustDecode("power."+string(tool), now, &p)
	data := &UPSDataset{URL: p.URL, Tool: tool}
	for _, d := range p.Devices {
		ups := nutUPS(d.Name, d.Model, d.Status, d.Charge, d.Runtime, d.Load, d.Power)
		if tool == enums.ServiceApcupsd {
			ups.OnBattery = strings.Contains(d.Status, "ONBATT")
		}
		data.Devices = append(data.Devices, ups)
	}
	return data
}

func DemoSolar(now time.Time) *SolarDataset {
	data := &SolarDataset{}
	demoworld.MustDecode("power.opendtu", now, data)
	return data
}

func DemoEVCC(now time.Time) *EVCCDataset {
	var p struct {
		EVCCDataset
		Stats30 struct{ ChargedKWh, AvgPrice, SolarPct float64 }
	}
	demoworld.MustDecode("power.evcc", now, &p)
	data := p.EVCCDataset
	data.ChargedKWh30, data.AvgPrice30, data.SolarPercent30 = p.Stats30.ChargedKWh, p.Stats30.AvgPrice, p.Stats30.SolarPct
	return &data
}

func init() {
	for _, s := range []source{PeaNUTData, ApcupsdData} {
		Register(s)
		Register(testOf{s, func(d any) map[string]any { return map[string]any{"devices": len(d.(*UPSDataset).Devices)} }})
	}
	Register(OpenDTUData)
	Register(testOf{OpenDTUData, func(d any) map[string]any { return map[string]any{"inverters": len(d.(*SolarDataset).Inverters)} }})
	Register(EVCCData)
	Register(testOf{EVCCData, func(d any) map[string]any { return map[string]any{"loadpoints": len(d.(*EVCCDataset).Loadpoints)} }})
}
