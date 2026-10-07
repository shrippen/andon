package rules_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// TestPrometheusAlert: each firing alert is a hint with the level of its
// severity label; pending ones wait.
func TestPrometheusAlert(t *testing.T) {
	data := &sources.PrometheusDataset{URL: "https://prom.example", Alerts: []sources.PromAlert{
		{Name: "HostDown", Severity: "critical", Instance: "nas:9100", State: "firing"},
		{Name: "DiskFull", Severity: "warning", Instance: "web:9100", State: "firing"},
		{Name: "Noisy", State: "firing"},
		{Name: "Soon", Severity: "critical", State: "pending"}}}
	got := run(t, "prometheus.alert", data, todayEnv(nil))
	if len(got) != 3 || got[0].Severity != enums.SeverityCritical || got[1].Severity != enums.SeverityWarn || got[2].Severity != enums.SeverityInfo {
		t.Fatalf("alerts: %+v", got)
	}
	if got[0].Fingerprint != "HostDown@nas:9100" || got[0].ActionURL != "https://prom.example/alerts" {
		t.Fatalf("first: %+v", got[0])
	}
}

// TestOutageCountsAlerts: a critical alert and a down monitor on the same
// host are one outage; the alert's own hint is suppressed, a warning is
// no outage signal.
func TestOutageCountsAlerts(t *testing.T) {
	prom := &sources.PrometheusDataset{Alerts: []sources.PromAlert{
		{Name: "HostDown", Severity: "critical", Instance: "nas.lan:9100", State: "firing"},
		{Name: "DiskFull", Severity: "warning", Instance: "web.lan:9100", State: "firing"}}}
	kuma := &sources.KumaDataset{Monitors: []sources.KumaMonitor{
		{Name: "NAS", Target: "https://nas.lan", Status: sources.KumaDown},
		{Name: "Web", Target: "https://web.lan", Status: sources.KumaDown}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"uptimekuma": kuma, "prometheus": prom}

	outages := rules.Outages(env)
	if len(outages) != 1 || len(outages["nas.lan"]) != 2 {
		t.Fatalf("outages: %v", outages)
	}
	alerts := run(t, "prometheus.alert", prom, env)
	if !rules.Suppressed(alerts[0], env, outages) || rules.Suppressed(alerts[1], env, outages) {
		t.Fatalf("suppression: %+v", alerts)
	}
}
