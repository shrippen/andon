package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestRoutesDown: the demo's vault route points at a container Docker
// does not run; a certificate ending in 9 days is a warning.
func TestRoutesDown(t *testing.T) {
	now := time.Now()
	env := todayEnv(nil)
	env.Datasets = map[string]any{"traefik": sources.DemoRoutes(now, enums.ServiceTraefik), "npm": sources.DemoRoutes(now, enums.ServiceNPM),
		"docker": sources.DemoDocker()}
	got := run(t, "routes.down", nil, env)
	if len(got) != 1 || got[0].Params["host"] != "vault.example.org" || got[0].Message != "routes.container_missing" {
		t.Fatalf("down: %+v", got)
	}
	if got := run(t, "routes.cert_expiry", nil, env); len(got) != 1 || got[0].Params["host"] != "kimai.example.org" {
		t.Fatalf("cert: %+v", got)
	}
}

// TestRouteUndocumented: a public host no IT note names.
func TestRouteUndocumented(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"caddy": &sources.RoutesDataset{Tool: enums.ServiceCaddy, Routes: []sources.ProxyRoute{{Host: "wiki.example.org", Up: true}, {Host: "photos.example.org", Up: true}}},
		"gitea": &sources.GiteaDataset{NotesRead: true, Notes: []sources.DocNote{{Name: "Immich", Web: "https://photos.example.org"}}},
	}
	if got := run(t, "cross.route_undocumented", nil, env); len(got) != 1 || got[0].Params["hosts"] != "wiki.example.org" {
		t.Fatalf("undocumented: %+v", got)
	}
}

// TestFritzAndLine: a fresh reconnect is a note; a test far below the sync
// rate points at the home network, a sync below the booked speed at the line.
func TestFritzAndLine(t *testing.T) {
	fritz := &sources.FritzDataset{Status: "Connected", Uptime: 2400, DownSync: 250000}
	if got := run(t, "fritz.reconnect", fritz, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("reconnect: %+v", got)
	}
	if got := run(t, "fritz.offline", &sources.FritzDataset{Status: "Disconnected"}, todayEnv(nil)); len(got) != 1 || got[0].Severity != enums.SeverityCritical {
		t.Fatalf("offline: %+v", got)
	}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"fritzbox": fritz, "speedtest": &sources.SpeedtestDataset{Down: 90}}
	if got := run(t, "cross.line_vs_speed", nil, env); len(got) != 1 || got[0].Message != "cross.speed_below_sync" {
		t.Fatalf("home: %+v", got)
	}
	env.Datasets = map[string]any{"fritzbox": &sources.FritzDataset{Status: "Connected", DownSync: 100000}, "speedtest": &sources.SpeedtestDataset{Down: 95, ExpectDown: 250}}
	if got := run(t, "cross.line_vs_speed", nil, env); len(got) != 1 || got[0].Message != "cross.sync_below_booked" {
		t.Fatalf("line: %+v", got)
	}
}

// TestDeviceUninventoried: clients of the network that no Snipe-IT asset
// names.
func TestDeviceUninventoried(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = map[string]any{
		"gateway": &sources.GatewayDataset{ClientNames: []string{"Schnitt-Laptop", "unknown-phone"}},
		"snipeit": &sources.SnipeDataset{Assets: []sources.SnipeAsset{{Name: "schnitt-laptop"}}},
	}
	if got := run(t, "cross.device_uninventoried", nil, env); len(got) != 1 || got[0].Params["names"] != "unknown-phone" {
		t.Fatalf("devices: %+v", got)
	}
}
