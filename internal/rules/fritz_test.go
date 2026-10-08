package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestFritzDemo: the demo box offers FRITZ!OS 8.21, its repeater hangs on
// a weak WLAN and waits for an update, the store room's thermostat lost
// its link, and a call was missed today.
func TestFritzDemo(t *testing.T) {
	box := sources.DemoFritz(time.Now())
	env := todayEnv(nil)
	env.Today = time.Now()
	for rule, want := range map[string]int{"fritz.update": 1, "fritz.update_error": 0, "fritz.mesh_update": 1, "fritz.mesh_weak": 1,
		"fritz.line_margin": 0, "fritz.missed_calls": 1, "fritz.smart_lost": 1} {
		if got := run(t, rule, box, env); len(got) != want {
			t.Errorf("%s: %+v", rule, got)
		}
	}
	if got := run(t, "fritz.smart_lost", box, env); len(got) == 1 && got[0].Severity != enums.SeverityWarn {
		t.Errorf("smart lost severity %v", got[0].Severity)
	}
}

// TestFritzLimits: margins and signal against the rules' limits; calls
// older than the window and a box without a call list stay quiet.
func TestFritzLimits(t *testing.T) {
	env := todayEnv(nil)
	low := &sources.FritzDataset{DownMargin: 5.5, UpMargin: 9}
	if got := run(t, "fritz.line_margin", low, env); len(got) != 1 {
		t.Fatalf("margin: %+v", got)
	}
	// Cable and fibre report no margin.
	if got := run(t, "fritz.line_margin", &sources.FritzDataset{}, env); len(got) != 0 {
		t.Fatalf("no margin: %+v", got)
	}
	mesh := &sources.FritzDataset{Mesh: []sources.FritzNode{{Name: "lan", Uplink: sources.FritzLAN}, {Name: "ok", Uplink: sources.FritzWLAN, Signal: -60}, {Name: "far", Uplink: sources.FritzWLAN, Signal: -80}}}
	if got := run(t, "fritz.mesh_weak", mesh, env); len(got) != 1 || got[0].Params["name"] != "far" {
		t.Fatalf("weak: %+v", got)
	}
	old := &sources.FritzDataset{Calls: &sources.FritzCalls{Missed: 1, Recent: []sources.FritzCall{{At: env.Today.AddDate(0, 0, -3)}}}}
	if got := run(t, "fritz.missed_calls", old, env); len(got) != 0 {
		t.Fatalf("old call: %+v", got)
	}
	if got := run(t, "fritz.missed_calls", &sources.FritzDataset{}, env); len(got) != 0 {
		t.Fatalf("no list: %+v", got)
	}
	if got := run(t, "fritz.update_error", &sources.FritzDataset{UpdateError: true}, env); len(got) != 1 || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("update error: %+v", got)
	}
}

// The FRITZ!Box's devices count as the network's clients too: online,
// no guests, not its own AVM devices.
func TestUninventoriedFritz(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"fritzbox": &sources.FritzDataset{Hosts: []sources.FritzHost{{Name: "Schnitt-Laptop", Active: true}, {Name: "Theos Tablet", Active: true},
			{Name: "Gast", Active: true, Guest: true}, {Name: "Repeater", Active: true, Model: "FRITZ!Repeater 1200 AX"}, {Name: "alt", Active: false}}},
		"snipeit": &sources.SnipeDataset{Assets: []sources.SnipeAsset{{Name: "schnitt-laptop"}}},
	}
	got := run(t, "cross.device_uninventoried", nil, env)
	if len(got) != 1 || got[0].Params["count"] != 1 || got[0].Params["names"] != "Theos Tablet" {
		t.Fatalf("%+v", got)
	}
	if src := got[0].Sources; len(src) != 2 || src[1] != "fritzbox" {
		t.Fatalf("sources %v", src)
	}
}
