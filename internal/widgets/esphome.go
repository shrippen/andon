package widgets

// "esphome": the dashboard's nodes, offline and outdated ones first; the
// dialog lists every node with platform, address and firmware.
//
//	4 nodes · 1 offline
//	● Teichpumpe        offline
//	● Garagentor        2026.6.2 → 2026.9.1

import (
	"sort"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// ESPHomeConfig is the "esphome" widget's config.
type ESPHomeConfig struct{ Limit int }

const espShown = 6

func init() {
	Tile[ESPHomeConfig]{Key: "esphome", Detail: dataDetail(espDetail), Category: CategoryInsight, Topic: TopicHome, Service: enums.ServiceESPHome, RefreshS: 300,
		Fields:  []Field{{Key: "limit", Input: InputNumber, Default: espShown, Min: "1", Max: "30"}},
		Decode:  func(r Raw) ESPHomeConfig { return ESPHomeConfig{Limit: r.Int("limit")} },
		Queries: ownData[ESPHomeConfig], View: dataView(espView),
		Calm: func(v map[string]any) bool { return v["Offline"] == 0 }}.add()
}

// ESPLine is a node with its state: "ok", "bad" (offline), "" (unknown).
type ESPLine struct {
	sources.ESPDevice
	State  string
	Behind bool
}

// espLines sorts offline nodes first, then outdated ones, then by name.
func espLines(data *sources.ESPHomeDataset) []ESPLine {
	var out []ESPLine
	for _, d := range data.Devices {
		line := ESPLine{ESPDevice: d, Behind: metrics.ESPBehind(d, data.Version)}
		if d.Online != nil {
			line.State = tierIf(*d.Online, "ok", "bad")
		}
		out = append(out, line)
	}
	rank := func(l ESPLine) int {
		switch {
		case l.State == "bad":
			return 0
		case l.Behind:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].Friendly < out[j].Friendly
	})
	return out
}

func espView(cfg ESPHomeConfig, data *sources.ESPHomeDataset, _ ViewCtx) map[string]any {
	lines := espLines(data)
	offline, behind := 0, 0
	for _, l := range lines {
		if l.State == "bad" {
			offline++
		}
		if l.Behind {
			behind++
		}
	}
	return map[string]any{"Total": len(lines), "Offline": offline, "Behind": behind, "Version": data.Version, "Lines": firstN(lines, cfg.Limit)}
}

func espDetail(_ ESPHomeConfig, data *sources.ESPHomeDataset, _ ViewCtx, results map[string]any) DetailView {
	lines := espLines(data)
	offline, behind := 0, 0
	var rows [][]Cell
	for _, l := range lines {
		state := Cell{Value: Txt("esphome.unknown")}
		switch l.State {
		case "bad":
			offline++
			state = Cell{Value: Txt("esphome.offline"), State: "bad"}
		case "ok":
			state = Cell{Value: Txt("esphome.online"), State: "ok"}
		}
		firmware := Cell{Value: orNone(l.Deployed)}
		if l.Behind {
			behind++
			firmware.State = "warn"
		}
		rows = append(rows, []Cell{{Value: l.Friendly}, {Value: orNone(l.Platform)}, {Value: orNone(l.Address)}, firmware, state})
	}
	body := &DetailBody{Facts: []Kpi{{Value: len(lines), Label: T("esphome.nodes")},
		{Value: offline, Label: T("esphome.offline"), Tier: tierIf(offline > 0, "red", "green")},
		{Value: behind, Label: T("esphome.behind"), Tier: tierIf(behind > 0, "yellow", "green")},
		{Value: orNone(data.Version), Label: T("esphome.version")}}}
	if len(rows) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("esphome.list"), Data: Table{
			Head: []Text{T("esphome.col.name"), T("esphome.col.platform"), T("esphome.col.address"), T("esphome.col.firmware"), T("esphome.col.state")}, Rows: rows}})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}
