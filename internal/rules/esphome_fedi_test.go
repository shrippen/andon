package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestESPHomeRules: the demo's pond pump is offline, the garage door
// behind; Home Assistant lost the workshop node. A node the dashboard
// cannot ping while Home Assistant gets values is the other case.
func TestESPHomeRules(t *testing.T) {
	now := time.Now().UTC()
	esp, ha := sources.DemoESPHome(now), sources.DemoHass(now)
	env := todayEnv(nil)
	if got := run(t, "esphome.offline", esp, env); len(got) != 1 || got[0].Params["name"] != "Teichpumpe" {
		t.Fatalf("offline: %+v", got)
	}
	if got := run(t, "esphome.update", esp, env); len(got) != 1 || got[0].Params["count"] != 1 {
		t.Fatalf("update: %+v", got)
	}
	env.Datasets = map[string]any{"esphome": esp, "homeassistant": ha}
	got := run(t, "cross.esphome_ha", nil, env)
	if len(got) != 1 || got[0].Message != "cross.esphome_ha_lost" || got[0].Params["entity"] != "sensor.werkstatt_klima_temperatur" {
		t.Fatalf("lost: %+v", got)
	}

	down := false
	esp.Devices = []sources.ESPDevice{{Name: "flur-sensor", Friendly: "Flur", Online: &down}}
	if got := run(t, "cross.esphome_ha", nil, env); len(got) != 1 || got[0].Message != "cross.esphome_ping" {
		t.Fatalf("ping: %+v", got)
	}
}

// TestFediverseRules: one unread mention; the website's release has no
// post, the clock's has one, the shot list's is too old.
func TestFediverseRules(t *testing.T) {
	now := time.Now().UTC()
	fedi := sources.DemoFediverse(now)
	env := todayEnv(nil)
	if got := run(t, "fediverse.mentions", fedi, env); len(got) != 1 || got[0].Params["count"] != 1 {
		t.Fatalf("mentions: %+v", got)
	}
	env.Datasets = map[string]any{"fediverse": fedi, "github": sources.DemoGitHub(now), "kdestore": sources.DemoKDEStore(now)}
	got := run(t, "cross.release_unannounced", nil, env)
	if len(got) != 1 || got[0].Params["project"] != "studio/website" || got[0].Params["version"] != "v0.12.0" {
		t.Fatalf("unannounced: %+v", got)
	}
	// Hints of a space are told apart by fingerprint alone: not the one
	// cross.release_red_ci gives the same release.
	if got[0].Fingerprint == "studio/website@v0.12.0" {
		t.Fatalf("fingerprint shared with cross.release_red_ci: %s", got[0].Fingerprint)
	}

	// A post naming the repo, or one released today, is no hint.
	fedi.Posts = append(fedi.Posts, sources.FediPost{Text: "New Website release!", At: now.Add(-time.Hour)})
	if got := run(t, "cross.release_unannounced", nil, env); len(got) != 0 {
		t.Fatalf("named: %+v", got)
	}
}
