package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// TestIntegrationRules runs each new rule on its demo dataset.
func TestIntegrationRules(t *testing.T) {
	env := todayEnv(nil)
	now := time.Now().UTC()
	cases := []struct {
		rule  string
		data  any
		count int
		level enums.Severity
	}{
		{"tailscale.key_expiry", sources.DemoTailscale(now), 1, enums.SeverityWarn},
		{"tailscale.offline", sources.DemoTailscale(now), 1, enums.SeverityInfo},
		{"gateway.wan_down", sources.DemoGateway(), 1, enums.SeverityCritical},
		{"gateway.updates", sources.DemoGateway(), 1, enums.SeverityInfo},
		{"gateway.devices_offline", sources.DemoGateway(), 0, 0},
		{"mediaserver.update", sources.DemoMediaServer(), 0, 0},
		{"arr.health", sources.DemoArr(now), 1, enums.SeverityInfo},
		{"arr.stuck", sources.DemoArr(now), 1, enums.SeverityWarn},
		{"vaultwarden.no_2fa", sources.DemoVaultwarden(now), 1, enums.SeverityWarn},
		{"speedtest.slow", &sources.SpeedtestDataset{Down: 90, Up: 40, ExpectDown: 250, At: now}, 1, enums.SeverityWarn},
		{"speedtest.slow", &sources.SpeedtestDataset{Down: 200, ExpectDown: 250, At: now}, 0, 0},
		{"grocy.expired", sources.DemoGrocy(now), 1, enums.SeverityWarn},
		{"grocy.missing", sources.DemoGrocy(now), 1, enums.SeverityInfo},
		{"grocy.chores_overdue", sources.DemoGrocy(now), 1, enums.SeverityInfo},
		{"dwd.warning", sources.DemoDWD(now), 1, enums.SeverityInfo},
		{"github.ci_failed", sources.DemoGitHub(now), 1, enums.SeverityWarn},
		{"github.review_waiting", sources.DemoGitHub(now), 1, enums.SeverityWarn},
		{"github.stale_pr", sources.DemoGitHub(now), 1, enums.SeverityInfo},
		{"energy.cost_rising", sources.DemoTibber(now), 0, 0},
	}
	for _, c := range cases {
		got := run(t, c.rule, c.data, env)
		if len(got) != c.count || (c.count > 0 && got[0].Severity != c.level) {
			t.Errorf("%s: %+v", c.rule, got)
		}
	}

	// A week costing 50 % more than the one before.
	rising := &sources.TibberDataset{Currency: "EUR"}
	for i := range 14 {
		cost := 2.0
		if i >= 7 {
			cost = 3
		}
		rising.Days = append(rising.Days, sources.EnergyDay{Cost: cost})
	}
	if got := run(t, "energy.cost_rising", rising, env); len(got) != 1 {
		t.Errorf("cost rising: %+v", got)
	}
	if topic := rules.RulesOf(rules.TopicUpdates); topic[len(topic)-1] != "mediaserver.update" {
		t.Errorf("updates topic: %v", topic)
	}
}

// An expired key says so; a device offline for a long time (an old
// laptop) is left to tailscale.offline instead of warning about its key.
func TestTailscaleKeyExpired(t *testing.T) {
	today := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	data := &sources.TailscaleDataset{Devices: []sources.TailDevice{
		{Name: "nas", Online: true, KeyExpiry: today.AddDate(0, 0, -3)},
		{Name: "laptop", Online: true, KeyExpiry: today.AddDate(0, 0, 5)},
		{Name: "old", Online: false, LastSeen: today.AddDate(-1, 0, 0), KeyExpiry: today.AddDate(0, -8, 0)},
	}}
	got := run(t, "tailscale.key_expiry", data, rules.Env{Today: today})
	if len(got) != 2 || got[0].Message != "tailscale.key_expired" || got[1].Message != "tailscale.key_expiry" {
		t.Fatalf("findings: %+v", got)
	}
}

// Known Sonarr/Radarr checks read as a short translated title; the raw
// English message stays in the explanation. Unknown checks stay as they
// are.
func TestArrHealthKnownChecks(t *testing.T) {
	data := &sources.ArrDataset{App: "Radarr", Health: []sources.ArrHealth{
		{Level: "warning", Source: "IndexerStatusCheck", Message: "Indexers unavailable due to failures for more than 6 hours: Elbindex"},
		{Level: "warning", Source: "SomethingNewCheck", Message: "Something new"},
	}}
	byMsg := map[string]rules.Finding{}
	for _, f := range run(t, "arr.health", data, todayEnv(nil)) {
		byMsg[f.Message] = f
	}
	known, ok := byMsg["arr.check"]
	if !ok || byMsg["arr.health"].Rule == "" {
		t.Fatalf("messages: %+v", byMsg)
	}
	if check, _ := known.Params["check"].(map[string]any); check["$t"] != "arr_check.IndexerStatusCheck" {
		t.Fatalf("check param: %+v", known.Params)
	}
}
