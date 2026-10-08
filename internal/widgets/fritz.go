package widgets

// "fritzbox": the FRITZ!Box at a glance and in its dialog.
//
//	tile    ↓ 12 ↑ 38 Mbit/s  · spark of the last 100 s
//	        ■ 9 devices online · 5 in WLAN
//	        ■ Repeater Tonstudio  −79 dBm   (WLAN uplink: signal, else rate)
//	        ■ 4 missed calls
//	        ■ Heizung Lager  not connected
//	        FRITZ!Box 7590 · FRITZ!OS 8.03            update 8.21
//
//	dialog  line facts, throughput chart; tabs devices, mesh, calls, Smart Home

import (
	"fmt"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// FritzConfig is the "fritzbox" widget's config: which parts the tile shows.
type FritzConfig struct {
	Traffic, Mesh, Calls, Smart bool
}

func init() {
	Tile[FritzConfig]{Key: "fritzbox", Detail: dataDetail(fritzDetail), Category: CategoryInsight, Topic: TopicNetwork, Service: enums.ServiceFritzBox, RefreshS: 5 * 60,
		Fields: []Field{{Key: "show_traffic", Input: InputCheck, Default: true}, {Key: "show_mesh", Input: InputCheck, Default: true},
			{Key: "show_calls", Input: InputCheck, Default: true}, {Key: "show_smart", Input: InputCheck, Default: true}},
		Decode: func(r Raw) FritzConfig {
			return FritzConfig{Traffic: r.Bool("show_traffic"), Mesh: r.Bool("show_mesh"), Calls: r.Bool("show_calls"), Smart: r.Bool("show_smart")}
		},
		Queries: ownData[FritzConfig], View: dataView(fritzView),
		Calm: func(v map[string]any) bool { return v["Calm"] == true }}.add()
}

// kbitPerMbit turns the box's kbit/s into Mbit/s.
const kbitPerMbit = 1000.0

// fritzLine is one row of the tile's list: a square in a tier, a name,
// a note, a value right.
type fritzLine struct {
	Tier  string
	Name  any
	Note  any
	Value any
}

func fritzView(cfg FritzConfig, data *sources.FritzDataset, ctx ViewCtx) map[string]any {
	weak := rules.Setting(ctx.Settings, "fritz.mesh_weak", "min_dbm")
	down, up := lastOf(data.TrafficDown), lastOf(data.TrafficUp)
	out := map[string]any{"Data": data, "Down": float64(down) / kbitPerMbit, "Up": float64(up) / kbitPerMbit,
		"Online": data.Connected(), "Traffic": cfg.Traffic && len(data.TrafficDown) > 0,
		"SyncDown": float64(data.DownSync) / kbitPerMbit, "SyncUp": float64(data.UpSync) / kbitPerMbit}
	if cfg.Traffic {
		out["Spark"] = SparkOf(floats(data.TrafficDown))
	}

	wlan := 0
	for _, h := range data.Hosts {
		if h.Active && h.Link == sources.FritzWLAN {
			wlan++
		}
	}
	lines := []fritzLine{{Tier: "green", Name: TxtA("fritz.devices", "n", data.Active()), Note: TxtA("fritz.wlan", "n", wlan)}}
	calm := data.Connected() && data.Update == "" && !data.UpdateError

	// Mesh nodes besides the box: their uplink; weak or waiting in yellow.
	updates := map[string]bool{}
	for _, name := range data.MeshUpdates() {
		updates[name] = true
	}
	for _, n := range data.Mesh {
		if !cfg.Mesh || n.Uplink == sources.FritzNoLink {
			continue
		}
		bad := fritzWeak(n, weak) || updates[n.Name]
		calm = calm && !bad
		lines = append(lines, fritzLine{Tier: tierIf(bad, "yellow", "green"), Name: n.Name, Value: fritzMeshValue(n)})
	}
	if c := data.Calls; cfg.Calls && c != nil && c.Missed > 0 {
		lines = append(lines, fritzLine{Tier: "yellow", Name: TxtA("fritz.missed", "n", c.Missed), Note: TxtA("fritz.days", "n", c.Days)})
	}
	if cfg.Smart {
		for _, d := range data.Smart {
			if !d.Present {
				calm = false
				lines = append(lines, fritzLine{Tier: "red", Name: d.Name, Note: Txt("fritz.smart_gone")})
			}
		}
		if watts, n := fritzPower(data.Smart); n > 0 {
			lines = append(lines, fritzLine{Tier: "green", Name: TxtA("fritz.smart", "n", len(data.Smart)), Value: NumU(watts, 0, "W")})
		}
	}
	out["Lines"], out["Calm"] = lines, calm
	return out
}

// fritzWeak reports a WLAN uplink below the rule's signal.
func fritzWeak(n sources.FritzNode, limit float64) bool {
	return n.Uplink == sources.FritzWLAN && n.Signal != 0 && float64(n.Signal) < limit
}

// fritzUplink names a node's uplink: "WLAN · −79 dBm", "Powerline".
func fritzUplink(n sources.FritzNode) any {
	if n.Signal != 0 {
		return TxtA("fritz.link_signal", "link", fritzLinkText(n.Uplink), "dbm", n.Signal)
	}
	return fritzLinkText(n.Uplink)
}

// fritzMeshValue is what a tile tells of a node: a WLAN uplink's signal,
// else its rate.
func fritzMeshValue(n sources.FritzNode) any {
	if n.Uplink == sources.FritzWLAN && n.Signal != 0 {
		return NumU(float64(n.Signal), 0, "dBm")
	}
	return fritzRate(n.Rate)
}

// fritzLinkText names a link: "LAN", "WLAN", "Powerline", "–".
func fritzLinkText(l sources.FritzLink) any {
	if l == sources.FritzNoLink {
		return Txt("fritz.link.none")
	}
	return Txt("fritz.link." + string(l))
}

// fritzRate is a link rate, "–" when unknown.
func fritzRate(mbit int) any {
	if mbit == 0 {
		return "–"
	}
	return NumU(float64(mbit), 0, "Mbit/s")
}

// fritzPower sums what the metering plugs draw now.
func fritzPower(list []sources.FritzSmart) (watts float64, meters int) {
	for _, d := range list {
		if d.Meter && d.Present {
			watts += d.Power
			meters++
		}
	}
	return watts, meters
}

func lastOf(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

func floats(values []int) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = float64(v)
	}
	return out
}

// ── dialog ──

// fritzStep is the online monitor's spacing.
const fritzStep = 5 * time.Second

// fritzDetail: the line and FRITZ!OS beside the throughput, then a tab
// per part the box has.
func fritzDetail(_ FritzConfig, data *sources.FritzDataset, ctx ViewCtx, results map[string]any) DetailView {
	weak := rules.Setting(ctx.Settings, "fritz.mesh_weak", "min_dbm")
	margin := rules.Setting(ctx.Settings, "fritz.line_margin", "min_db")
	fw := data.Firmware
	if data.Update != "" {
		fw = fmt.Sprintf("%s → %s", data.Firmware, data.Update)
	}
	body := &DetailBody{Side: []Fact{{Label: T("detail.fritz.model"), Value: data.Model},
		{Label: T("detail.fritz.firmware"), Value: fw, State: stateIf(data.Update != "" || data.UpdateError, "warn")},
		{Label: T("detail.fritz.since"), Value: agoOf(data.Since)}}}
	if data.UpdateError {
		body.Side = append(body.Side, Fact{Label: T("detail.fritz.update"), Value: Txt("detail.fritz.update_error"), State: "warn"})
	}
	if data.DownSync > 0 {
		body.Side = append(body.Side, Fact{Label: T("detail.fritz.sync"), Value: fmt.Sprintf("%.0f / %.0f Mbit/s", float64(data.DownSync)/kbitPerMbit, float64(data.UpSync)/kbitPerMbit)},
			Fact{Label: T("detail.fritz.max"), Value: fmt.Sprintf("%.0f / %.0f Mbit/s", float64(data.DownMax)/kbitPerMbit, float64(data.UpMax)/kbitPerMbit)})
	}
	if data.DownMargin > 0 {
		low := data.DownMargin < margin || (data.UpMargin > 0 && data.UpMargin < margin)
		body.Side = append(body.Side, Fact{Label: T("detail.fritz.margin"), Value: fmt.Sprintf("%.1f / %.1f dB", data.DownMargin, data.UpMargin), State: stateIf(low, "warn")})
	}

	body.Facts = []Kpi{{Value: NumU(float64(lastOf(data.TrafficDown))/kbitPerMbit, 1, "Mbit/s"), Label: T("detail.fritz.down")},
		{Value: NumU(float64(lastOf(data.TrafficUp))/kbitPerMbit, 1, "Mbit/s"), Label: T("detail.fritz.up")},
		{Value: data.Active(), Label: T("detail.fritz.online")}}
	if c := data.Calls; c != nil {
		body.Facts = append(body.Facts, Kpi{Value: c.Missed, Label: textArgs("detail.fritz.missed", "n", c.Days), Tier: tierIf(c.Missed > 0, "yellow", "")})
	}
	if g, ok := fritzTrafficGraph(data); ok {
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("detail.fritz.traffic"), Hero: true, Data: g})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)

	body.Tabs = append(body.Tabs, fritzHostsTab(data))
	if len(data.Mesh) > 0 || len(data.Radios) > 0 {
		body.Tabs = append(body.Tabs, fritzMeshTab(data, weak))
	}
	if data.Calls != nil {
		body.Tabs = append(body.Tabs, fritzCallsTab(data.Calls))
	}
	if len(data.Smart) > 0 {
		body.Tabs = append(body.Tabs, fritzSmartTab(data.Smart))
	}

	head := DetailHead{State: "ok", StateKey: "detail.fritz.connected", Actions: []DetailAction{{LabelKey: "detail.open_in", Href: data.URL, Primary: true}}}
	if !data.Connected() {
		head.State, head.StateKey = "bad", "detail.fritz.offline"
	}
	return zoned(DetailView{Head: head, Body: body}, time.Local)
}

// fritzTrafficGraph: down and up over the last 100 s, in Mbit/s.
func fritzTrafficGraph(data *sources.FritzDataset) (Graph, bool) {
	n := len(data.TrafficDown)
	if n < minPoints {
		return Graph{}, false
	}
	down, up := floats(data.TrafficDown), floats(data.TrafficUp)
	for i := range down {
		down[i] /= kbitPerMbit
	}
	for i := range up {
		up[i] /= kbitPerMbit
	}
	g := LineGraph(Series{Values: down, Class: "s1", Label: Txt("detail.fritz.down")}, Series{Values: up, Class: "s2", Label: Txt("detail.fritz.up")})
	g.Lo, g.Unit = 0, "Mbit/s"
	for i := range n {
		g.Labels = append(g.Labels, TxtA("detail.fritz.ago_s", "n", int((time.Duration(n-1-i)*fritzStep).Seconds())))
	}
	g.Ticks = []any{g.Labels[0], Txt("detail.fritz.now")}
	return g, true
}

func fritzHostsTab(data *sources.FritzDataset) Tab {
	var rows [][]Cell
	var away []string
	for _, h := range data.Hosts {
		if !h.Active {
			away = append(away, h.Name)
			continue
		}
		link := fritzLinkText(h.Link)
		if h.Guest {
			link = TxtA("detail.fritz.guest", "link", link)
		}
		signal := any("–")
		if h.Signal != 0 {
			signal = NumU(float64(h.Signal), 0, "dBm")
		}
		rows = append(rows, []Cell{{Value: h.Name}, {Value: h.IP}, {Value: link}, {Value: fritzRate(h.Speed)}, {Value: signal}})
	}
	blocks := []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.fritz.device"), T("detail.fritz.ip"), T("detail.fritz.link"),
		T("detail.fritz.rate"), T("detail.fritz.signal")}, Rows: rows, Num: []int{3, 4}}}}
	if len(away) > 0 {
		blocks = append(blocks, Block{Kind: BlockChips, Label: T("detail.fritz.away"), Meta: len(away), Data: away})
	}
	return Tab{Label: T("detail.fritz.devices"), Count: len(rows), Blocks: blocks}
}

func fritzMeshTab(data *sources.FritzDataset, weak float64) Tab {
	updates := map[string]bool{}
	for _, name := range data.MeshUpdates() {
		updates[name] = true
	}
	var rows [][]Cell
	for _, n := range data.Mesh {
		role := any("–")
		if n.Role != "" {
			role = Txt("detail.fritz.role_" + n.Role)
		}
		uplink := any("–")
		if n.Uplink != sources.FritzNoLink {
			uplink = fritzUplink(n)
		}
		fw := Cell{Value: n.Firmware}
		if updates[n.Name] {
			fw = Cell{Value: TxtA("detail.fritz.fw_update", "v", n.Firmware), State: "warn"}
		}
		rows = append(rows, []Cell{{Value: n.Name}, {Value: n.Model}, fw, {Value: role},
			{Value: uplink, State: stateIf(fritzWeak(n, weak), "warn")}, {Value: fritzRate(n.Rate)}, {Value: n.Clients}})
	}
	blocks := []Block{{Kind: BlockTable, Data: Table{Head: []Text{T("detail.fritz.node"), T("detail.fritz.model"), T("detail.fritz.firmware"),
		T("detail.fritz.role"), T("detail.fritz.uplink"), T("detail.fritz.rate"), T("detail.fritz.clients")}, Rows: rows, Num: []int{5, 6}}}}

	var radios []LitRow
	for _, r := range data.Radios {
		state, meta := "off", any(Txt("detail.fritz.radio_off"))
		if r.On {
			state, meta = "ok", TxtA("detail.fritz.radio_on", "channel", r.Channel, "n", r.Clients)
		}
		radios = append(radios, LitRow{Name: r.Band + " GHz", Meta: meta, State: state})
	}
	if len(radios) > 0 {
		blocks = append(blocks, Block{Kind: BlockRows, Label: T("detail.fritz.radios"), Data: radios})
	}
	return Tab{Label: T("detail.fritz.mesh"), Count: len(data.Mesh), Blocks: blocks}
}

func fritzCallsTab(c *sources.FritzCalls) Tab {
	total := max(c.In+c.Out+c.Missed, 1)
	share := func(n int) float64 { return float64(n) / float64(total) * percentScale }
	bars := []ShareBar{{Name: Txt("detail.fritz.calls_in"), Pct: share(c.In), Value: c.In, Tier: "green"},
		{Name: Txt("detail.fritz.calls_out"), Pct: share(c.Out), Value: c.Out},
		{Name: Txt("detail.fritz.calls_missed"), Pct: share(c.Missed), Value: c.Missed, Tier: tierIf(c.Missed > 0, "yellow", "")}}
	blocks := []Block{{Kind: BlockBars, Label: textArgs("detail.fritz.calls_days", "n", c.Days), Data: bars}}

	var events []Event
	for _, call := range c.Recent {
		who := any(call.Who)
		if call.Who == "" {
			who = Txt("detail.fritz.unknown_caller")
		}
		events = append(events, Event{At: call.At, Title: who, Tier: "yellow"})
	}
	if len(events) > 0 {
		blocks = append(blocks, Block{Kind: BlockTimeline, Label: T("detail.fritz.missed_recent"), Data: events})
	}
	return Tab{Label: T("detail.fritz.calls"), Count: c.Missed, Blocks: blocks}
}

func fritzSmartTab(list []sources.FritzSmart) Tab {
	var rows [][]Cell
	dash := func(ok bool, v float64, digits int, unit string) any {
		if !ok {
			return "–"
		}
		return NumU(v, digits, unit)
	}
	for _, d := range list {
		state := Cell{Value: Txt("detail.fritz.present"), State: "ok"}
		if !d.Present {
			state = Cell{Value: Txt("fritz.smart_gone"), State: "bad"}
		}
		sw := any("–")
		if d.Switch != "" {
			sw = Txt("detail.fritz.switch_" + d.Switch)
		}
		rows = append(rows, []Cell{{Value: d.Name}, {Value: d.Product}, state, {Value: sw}, {Value: dash(d.Meter, d.Power, 1, "W")},
			{Value: dash(d.Meter, d.Energy, 1, "kWh")}, {Value: dash(d.Thermo, d.Temp, 1, "°C")}, {Value: dash(d.Heater, d.Set, 1, "°C")}})
	}
	head := []Text{T("detail.fritz.device"), T("detail.fritz.product"), T("detail.state"), T("detail.fritz.switch"), T("detail.fritz.power"),
		T("detail.fritz.energy"), T("detail.fritz.temp"), T("detail.fritz.set")}
	return Tab{Label: T("detail.fritz.smart"), Count: len(list), Blocks: []Block{{Kind: BlockTable, Data: Table{Head: head, Rows: rows, Num: []int{4, 5, 6, 7}}}}}
}
