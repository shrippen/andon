package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestFritzTile: the demo box is not calm (update, weak repeater, lost
// thermostat); the lines name them; parts the user turned off are gone.
func TestFritzTile(t *testing.T) {
	box := sources.DemoFritz(time.Now())
	all := FritzConfig{Traffic: true, Mesh: true, Calls: true, Smart: true}
	v := fritzView(all, box, ViewCtx{})
	if v["Calm"] != false || v["Spark"] == nil || v["Online"] != true {
		t.Fatalf("view %+v", v)
	}
	tiers := map[string]int{}
	for _, l := range v["Lines"].([]fritzLine) {
		tiers[l.Tier]++
	}
	// Devices, mesh (repeater weak, powerline fine), missed calls, the
	// lost thermostat, the plugs' power.
	if tiers["yellow"] != 2 || tiers["red"] != 1 || tiers["green"] != 3 {
		t.Fatalf("lines %+v", v["Lines"])
	}
	bare := fritzView(FritzConfig{}, box, ViewCtx{})
	if lines := bare["Lines"].([]fritzLine); len(lines) != 1 || bare["Spark"] != nil {
		t.Fatalf("bare %+v", bare)
	}
}

// TestFritzDialog: the throughput as the hero chart with its axis, and a
// tab per part: devices, mesh, calls, Smart Home.
func TestFritzDialog(t *testing.T) {
	box := sources.DemoFritz(time.Now())
	view := fritzDetail(FritzConfig{}, box, ViewCtx{}, map[string]any{})
	body := view.Body.(*DetailBody)
	if view.Head.State != "ok" || view.Head.Zone == "" || len(body.Tabs) != 4 {
		t.Fatalf("head %+v tabs %d", view.Head, len(body.Tabs))
	}
	g, ok := body.Blocks[0].Data.(Graph)
	if !ok || len(g.Series) != 2 || g.Unit != "Mbit/s" || len(g.Labels) != len(box.TrafficDown) || len(g.Ticks) != 2 {
		t.Fatalf("graph %+v", body.Blocks[0])
	}
	if tab := body.Tabs[0]; tab.Count != box.Active() {
		t.Fatalf("devices %+v", tab)
	}
	// A box without calls, Smart Home and mesh has the devices tab only.
	plain := fritzDetail(FritzConfig{}, &sources.FritzDataset{Status: "Disconnected"}, ViewCtx{}, map[string]any{})
	if plain.Head.State != "bad" || len(plain.Body.(*DetailBody).Tabs) != 1 {
		t.Fatalf("plain %+v", plain)
	}
}
