package widgets

// "power": the space's power at a glance: every UPS (PeaNUT, apcupsd)
// with charge and runtime, the panels (OpenDTU) and the wallbox (EVCC).
// The dialog lists each device and EVCC's last 30 days.

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

// wattsPerKilo turns W into kW and Wh into kWh.
const wattsPerKilo = 1000.0

// PowerConfig is the "power" widget's config.
type PowerConfig struct{}

// powerServices are the sources the tile reads.
var powerServices = []enums.ServiceType{enums.ServicePeaNUT, enums.ServiceApcupsd, enums.ServiceOpenDTU, enums.ServiceEVCC}

func init() {
	Tile[PowerConfig]{Key: "power", Detail: powerDetail, Category: CategoryInsight, Topic: TopicHome, RefreshS: 120,
		Decode: func(Raw) PowerConfig { return PowerConfig{} },
		Queries: func(PowerConfig) []Query {
			var out []Query
			for _, s := range powerServices {
				out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
			}
			return out
		},
		View: powerView,
		Calm: func(v map[string]any) bool { return v["Any"] == true && v["Trouble"] == false }}.add()
}

// powerData is what the tile and dialog read.
type powerData struct {
	UPS   []sources.UPS
	Solar *sources.SolarDataset
	EVCC  *sources.EVCCDataset
}

func powerOf(results map[string]any) powerData {
	var p powerData
	for _, s := range []enums.ServiceType{enums.ServicePeaNUT, enums.ServiceApcupsd} {
		if d, ok := results[string(s)].(*sources.UPSDataset); ok {
			p.UPS = append(p.UPS, d.Devices...)
		}
	}
	p.Solar, _ = results[string(enums.ServiceOpenDTU)].(*sources.SolarDataset)
	p.EVCC, _ = results[string(enums.ServiceEVCC)].(*sources.EVCCDataset)
	return p
}

// upsPill is the Kante pill of a UPS: on battery red, short or old
// battery yellow, else green.
func upsPill(u sources.UPS) string {
	switch {
	case u.OnBattery:
		return "failed"
	case u.ReplaceBattery || u.LowBattery:
		return "locked"
	}
	return "applied"
}

func powerView(_ PowerConfig, results map[string]any, _ ViewCtx) map[string]any {
	p := powerOf(results)
	trouble := false
	for _, u := range p.UPS {
		trouble = trouble || u.OnBattery || u.ReplaceBattery
	}
	if p.Solar != nil {
		for _, inv := range p.Solar.Inverters {
			trouble = trouble || !inv.Reachable
		}
	}
	type chargeLine struct {
		sources.Loadpoint
		KW float64
	}
	var charging []chargeLine
	if p.EVCC != nil {
		for _, lp := range p.EVCC.Loadpoints {
			if lp.Charging {
				charging = append(charging, chargeLine{Loadpoint: lp, KW: lp.ChargePower / wattsPerKilo})
			}
		}
	}
	type upsLine struct {
		sources.UPS
		Pill    string
		Minutes int
	}
	var lines []upsLine
	for _, u := range p.UPS {
		lines = append(lines, upsLine{UPS: u, Pill: upsPill(u), Minutes: u.Runtime / 60})
	}
	out := map[string]any{"Any": len(p.UPS) > 0 || p.Solar != nil || p.EVCC != nil, "Trouble": trouble, "UPS": lines, "Charging": charging}
	if p.Solar != nil {
		out["PV"], out["YieldKWh"] = p.Solar.Power, p.Solar.YieldDay/wattsPerKilo
	}
	return out
}

func powerDetail(_ PowerConfig, results map[string]any, _ ViewCtx) DetailView {
	p := powerOf(results)
	body := &DetailBody{}
	if p.Solar != nil {
		body.Facts = append(body.Facts, Kpi{Value: NumU(p.Solar.Power, 0, "W"), Label: T("power.pv_now"), Tier: "green"},
			Kpi{Value: NumU(p.Solar.YieldDay/wattsPerKilo, 1, "kWh"), Label: T("power.pv_today")})
	}
	if p.EVCC != nil {
		body.Facts = append(body.Facts, Kpi{Value: NumU(p.EVCC.Grid, 0, "W"), Label: T("power.grid"), Tier: tierIf(p.EVCC.Grid > 0, "yellow", "green")},
			Kpi{Value: NumU(p.EVCC.Home, 0, "W"), Label: T("power.home")})
	}
	if len(p.UPS) > 0 {
		var rows [][]Cell
		for _, u := range p.UPS {
			state := map[string]string{"failed": "bad", "locked": "warn"}[upsPill(u)]
			rows = append(rows, []Cell{{Value: u.Name}, {Value: u.Model}, {Value: u.Status, State: state}, {Value: NumU(u.Charge, 0, "%")},
				{Value: TxtA("power.minutes", "n", u.Runtime/60)}, {Value: NumU(u.Load, 0, "%")}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("power.ups"), Data: Table{
			Head: []Text{T("power.col.name"), T("power.col.model"), T("power.col.status"), T("power.col.charge"), T("power.col.runtime"), T("power.col.load")},
			Rows: rows, Num: []int{3, 4, 5}}})
	}
	if p.Solar != nil && len(p.Solar.Inverters) > 0 {
		var rows [][]Cell
		for _, inv := range p.Solar.Inverters {
			rows = append(rows, []Cell{{Value: inv.Name}, {Value: Txt(map[bool]string{true: "power.reachable", false: "power.unreachable"}[inv.Reachable]),
				State: stateIf(!inv.Reachable, "bad")}, {Value: NumU(inv.Power, 0, "W")}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("power.inverters"), Data: Table{
			Head: []Text{T("power.col.name"), T("power.col.state"), T("power.col.power")}, Rows: rows, Num: []int{2}}})
	}
	if p.EVCC != nil {
		var rows [][]Cell
		for _, lp := range p.EVCC.Loadpoints {
			rows = append(rows, []Cell{{Value: lp.Title}, {Value: orNone(lp.Vehicle)}, {Value: NumU(lp.ChargePower/wattsPerKilo, 1, "kW")},
				{Value: NumU(lp.VehicleSoc, 0, "%")}, {Value: NumU(lp.Charged, 1, "kWh")}, {Value: orNone(lp.Mode)}})
		}
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("power.loadpoints"), Data: Table{
			Head: []Text{T("power.col.name"), T("power.col.vehicle"), T("power.col.power"), T("power.col.soc"), T("power.col.charged"), T("power.col.mode")},
			Rows: rows, Num: []int{2, 3, 4}}})
		if p.EVCC.ChargedKWh30 > 0 {
			body.Side = append(body.Side, Fact{Label: T("power.charged30"), Value: NumU(p.EVCC.ChargedKWh30, 0, "kWh")},
				Fact{Label: T("power.solar30"), Value: NumU(p.EVCC.SolarPercent30, 0, "%")},
				Fact{Label: T("power.price30"), Value: NumU(p.EVCC.AvgPrice30*100, 0, "ct/kWh")})
		}
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
