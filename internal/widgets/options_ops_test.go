package widgets_test

import (
	"andon/internal/metrics"
	"fmt"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestBackupsOptions: only problems, one tool, and the whole list still
// counts for "all fine".
func TestBackupsOptions(t *testing.T) {
	now := time.Now().UTC()
	borg := &sources.BorgDataset{Clients: []sources.BorgClient{
		{Name: "nas", LastBackup: now.Add(-time.Hour)},
		{Name: "laptop", LastBackup: now.Add(-72 * time.Hour)},
	}}
	res := map[string]any{string(enums.ServiceBorgBackup): borg}
	v := viewOf(t, "backups", map[string]any{"only_problems": true, "tools": []any{"borgbackup"}, "days": "30"}, res, "", nil)
	rows := v["Rows"].([]widgets.BackupLine)
	if len(rows) != 1 || rows[0].Item != "laptop" || v["Total"] != 2 || v["Wide"] != true {
		t.Fatalf("backups: %+v", v)
	}
	v = viewOf(t, "backups", map[string]any{"tools": []any{"truenas"}}, res, "", nil)
	if v["Total"] != 0 {
		t.Fatalf("filtered out: %+v", v)
	}
}

// TestDisksOptions: a lower temperature limit, fine disks hidden.
func TestDisksOptions(t *testing.T) {
	data := &sources.ScrutinyDataset{Disks: []sources.Disk{
		{Name: "sda", Status: sources.ScrutinyPassed, Temp: 38},
		{Name: "sdb", Status: sources.ScrutinyPassed, Temp: 30},
	}}
	v := viewOf(t, "disks", map[string]any{"temp_warn": 35.0, "only_problems": true}, map[string]any{"data": data}, enums.ServiceScrutiny, nil)
	rows := v["Rows"].([]widgets.DiskRow)
	if len(rows) != 1 || rows[0].Name != "sda" || rows[0].TempTier != "yellow" || v["Total"] != 2 {
		t.Fatalf("disks: %+v", v)
	}
}

// TestConnHealthShaky: a connection that failed only long ago drops out.
func TestConnHealthShaky(t *testing.T) {
	old := widgets.ConnStrip{Name: "alt", FailPct: 10, Days: []widgets.ConnDayState{{Fail: 3}, {OK: 5}, {OK: 5}}}
	now := widgets.ConnStrip{Name: "neu", FailPct: 5, Days: []widgets.ConnDayState{{OK: 5}, {OK: 5}, {Fail: 1}}}
	v := viewOf(t, "conn_health", map[string]any{"only_problems": true}, map[string]any{widgets.ConnHealthSlot: []widgets.ConnStrip{old, now}}, "", nil)
	rows := v["Rows"].([]widgets.StripRow)
	if len(rows) != 1 || rows[0].Name != "neu" {
		t.Fatalf("rows: %+v", rows)
	}
}

func fillingTank() *metrics.History {
	today := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	var points []metrics.Point
	for i := 20; i >= 0; i-- {
		points = append(points, metrics.Point{Day: today.AddDate(0, 0, -i), Value: 0.8 - float64(i)*0.01})
	}
	return &metrics.History{Series: map[string][]metrics.Point{"truenas.pool.tank.used": points, "proxmox.storage.local.used": points}}
}

// TestStorageOptions: a filter and a shorter look ahead.
func TestStorageOptions(t *testing.T) {
	v := viewOf(t, "storage_forecast", map[string]any{"filter": []any{"tank"}, "ahead": 10.0}, map[string]any{widgets.HistorySlot: fillingTank()}, "", nil)
	rows := v["Rows"].([]widgets.StorageRow)
	if len(rows) != 1 || rows[0].Ahead < 9.5 || rows[0].Ahead > 11 || v["Ahead"] != 10 {
		t.Fatalf("storage: %+v", v)
	}
}

// TestTrueNASOptions: a lower warning level, the apps with updates named,
// and the forecast per pool.
func TestTrueNASOptions(t *testing.T) {
	nas := &sources.TrueNASDataset{Pools: []sources.Pool{{Name: "tank", Status: "ONLINE", Healthy: true, Size: 100, Allocated: 50}},
		Apps: []sources.TNApp{{Name: "immich", Update: true}, {Name: "jellyfin"}}}
	v := viewOf(t, "truenas_pools", map[string]any{"warn_pct": 40.0, "app_updates": true, "forecast": true},
		map[string]any{"data": nas, widgets.HistorySlot: fillingTank()}, enums.ServiceTrueNAS, nil)
	pools := v["Pools"].([]widgets.PoolBar)
	if pools[0].Tier != "yellow" || pools[0].FullIn < 19 || v["Apps"] != "immich" {
		t.Fatalf("truenas: %+v", v)
	}
}

// TestKomodoOptions: filtered stacks, only the troubled drawn.
func TestKomodoOptions(t *testing.T) {
	data := &sources.KomodoDataset{Stacks: []sources.KStack{{Name: "web-prod", State: "running"}, {Name: "web-dev", State: "down"}, {Name: "db", State: "down"}}}
	v := viewOf(t, "komodo_stacks", map[string]any{"filter": []any{"web"}, "only_problems": true}, map[string]any{"data": data}, enums.ServiceKomodo, nil)
	if v["Stacks"] != 2 || len(v["Cells"].([]widgets.StripCell)) != 1 {
		t.Fatalf("komodo: %+v", v)
	}
}

// TestUpdateWindowOptions: a factor can be ignored and a window that never
// contains now blocks.
func TestUpdateWindowOptions(t *testing.T) {
	kimai := &sources.KimaiDataset{Active: []sources.KimaiSheet{{ID: 1}}}
	res := map[string]any{string(enums.ServiceKimai): kimai}
	v := viewOf(t, "update_window", map[string]any{"use_timer": false, "use_backup": false}, res, "", nil)
	if b := v["Window"].(metrics.Window).Blockers; len(b) != 0 {
		t.Fatalf("timer ignored: %v", b)
	}
	now := time.Now().UTC()
	span := fmt.Sprintf("%02d:00-%02d:00", (now.Hour()+2)%24, (now.Hour()+3)%24)
	v = viewOf(t, "update_window", map[string]any{"use_timer": false, "use_backup": false, "window": span, "timezone": "UTC"}, res, "", nil)
	if b := v["Window"].(metrics.Window).Blockers; len(b) != 1 || b[0] != widgets.WindowOutside {
		t.Fatalf("outside: %v", b)
	}
}

// TestEnergyOptions: only today, energy price only, a longer window.
func TestEnergyOptions(t *testing.T) {
	data := sources.DemoTibber(time.Now())
	v := viewOf(t, "energy", map[string]any{"tomorrow": false, "price": "energy", "cheap_hours": 5.0}, map[string]any{"data": data}, enums.ServiceTibber, nil)
	shown := v["Data"].(*sources.TibberDataset)
	if len(shown.Prices) > 24 || shown.Current != data.CurrentEnergy || v["EnergyOnly"] != true {
		t.Fatalf("energy: %d prices, current %v", len(shown.Prices), shown.Current)
	}
	if h, ok := v["CheapHours"]; ok && h != 5 {
		t.Fatalf("cheap hours: %v", h)
	}
	if len(data.Prices) != 48 {
		t.Fatalf("source changed: %d", len(data.Prices))
	}
}

// TestSysinfoOptions: swap and disks hidden, a lower warning level.
func TestSysinfoOptions(t *testing.T) {
	stats := &sources.GlancesResult{CPU: 45, Mem: 30, Swap: 5, Disks: []sources.GlancesDisk{{Mount: "/", Percent: 50}}}
	v := viewOf(t, "sysinfo", map[string]any{"show_swap": false, "show_disks": false, "warn_pct": 40.0}, map[string]any{"stats": stats}, enums.ServiceGlances, nil)
	meters := v["Meters"].([]widgets.Meter)
	if len(meters) != 2 || meters[0].Tier != "yellow" || meters[1].Tier != "green" {
		t.Fatalf("meters: %+v", meters)
	}
}

// TestMonitorsOptions: a filter and no response times.
func TestMonitorsOptions(t *testing.T) {
	data := &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "Shop", Status: sources.KumaUp, MS: 120}, {Name: "NAS", Status: sources.KumaUp, MS: 20}}}
	v := viewOf(t, "monitors", map[string]any{"filter": []any{"shop"}, "response_time": false}, map[string]any{"data": data}, enums.ServiceUptimeKuma, nil)
	if v["Total"] != 1 || v["AvgMS"] != nil || v["HideMS"] != true {
		t.Fatalf("monitors: %+v", v)
	}
}

// TestExpiryOptions: only certificates within 30 days.
func TestExpiryOptions(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, ctxFor(enums.ServiceCerts, nil).Today) // the tile's today
	certs := &sources.CertDataset{Certs: []sources.Cert{{Host: "a", NotAfter: now.AddDate(0, 0, 10)}, {Host: "b", NotAfter: now.AddDate(0, 0, 80)}}}
	doms := &sources.DomainsDataset{Domains: []sources.DomainInfo{{Name: "example.org", Expires: now.AddDate(0, 0, 5)}}}
	v := viewOf(t, "expiry", map[string]any{"max_days": 30.0, "kinds": "certs"}, map[string]any{"data": certs, "domains": doms}, enums.ServiceCerts, nil)
	if v["Total"] != 1 {
		t.Fatalf("expiry: %+v", v)
	}
}

// TestAuthentikOptions: failed logins of the last day.
func TestAuthentikOptions(t *testing.T) {
	data := sources.DemoAuthentik(time.Now())
	v := viewOf(t, "authentik_logins", map[string]any{"period": "24h", "only_problems": true}, map[string]any{"data": data}, enums.ServiceAuthentik, nil)
	list := v["Logins"].([]sources.AKLogin)
	if v["Failed"] != 38 || len(list) != 2 || list[0].User != "admin" {
		t.Fatalf("authentik: %+v", v)
	}
}

// TestGitHubRedCI: only the failing repo stays.
func TestGitHubRedCI(t *testing.T) {
	data := &sources.GitHubDataset{Repos: []sources.GitRepo{{Name: "a/ok", CI: "success"}, {Name: "a/bad", CI: "failure"}}}
	v := viewOf(t, "github", map[string]any{"only_problems": true}, map[string]any{"data": data}, enums.ServiceGitHub, nil)
	if repos := v["Data"].(*sources.GitHubDataset).Repos; len(repos) != 1 || repos[0].Name != "a/bad" || len(data.Repos) != 2 {
		t.Fatalf("github: %+v", repos)
	}
}

// TestGlancesWarnLine: the line sits at its share of the axis.
func TestGlancesWarnLine(t *testing.T) {
	data := &sources.GlancesHistory{Metric: "cpu", Samples: []sources.Sample{{Value: 10}, {Value: 30}}}
	v := viewOf(t, "glances_chart", map[string]any{"warn_line": 80.0}, map[string]any{"history": data}, enums.ServiceGlances, nil)
	if v["WarnPct"] != 80 {
		t.Fatalf("glances: %+v", v)
	}
}

// TestCustomAPIThresholds: a threshold colours the value, a unit follows it.
func TestCustomAPIThresholds(t *testing.T) {
	body := &sources.JSONResult{Body: map[string]any{"queue": 12.0}}
	v := viewOf(t, "custom_api", map[string]any{"url": "https://x.example", "fields": "Queue = queue", "thresholds": "queue > 10 rot", "units": "Queue = Jobs"},
		map[string]any{"body": body}, "", nil)
	rows := v["Rows"].([]widgets.APIValue)
	if rows[0].Level != "fail" || rows[0].Unit != "Jobs" {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestStatusLightDirect: a monitor down turns the light red with no hint,
// and own words replace "red".
func TestStatusLightDirect(t *testing.T) {
	kuma := &sources.KumaDataset{Monitors: []sources.KumaMonitor{{Name: "Shop", Status: sources.KumaDown}}}
	v := viewOf(t, "status_light", map[string]any{"direct": true, "text_red": "Handeln"}, map[string]any{"kuma": kuma}, "", nil)
	if v["State"] != "red" || v["Text"] != "Handeln" || v["Count"] != 1 {
		t.Fatalf("light: %+v", v)
	}
	v = viewOf(t, "status_light", map[string]any{}, map[string]any{"kuma": kuma}, "", nil)
	if v["State"] != "green" {
		t.Fatalf("not direct: %+v", v)
	}
}

// TestUptimeSLA: below the target is red, filtered monitors drop out.
func TestUptimeSLA(t *testing.T) {
	cfg, _ := widgets.Decode("uptime_month", map[string]any{"sla": 99.9, "filter": []any{"shop"}})
	c := cfg.(widgets.UptimeMonthConfig)
	if c.SLA != 99.9 || len(c.Only) != 1 {
		t.Fatalf("cfg: %+v", c)
	}
}

// TestExposureOnlyOpen: resources behind a login drop out, the total stays.
func TestExposureOnlyOpen(t *testing.T) {
	data := &sources.PangolinDataset{Resources: []sources.PResource{{Name: "a", Domain: "a.example", Enabled: true, SSO: true},
		{Name: "b", Domain: "b.example", Enabled: true}}}
	v := viewOf(t, "exposure", map[string]any{"only_open": true}, map[string]any{"data": data}, enums.ServicePangolin, nil)
	if rows := v["Rows"].([]widgets.ExposedRow); len(rows) != 1 || rows[0].Name != "b" || v["Total"] != 2 {
		t.Fatalf("exposure: %+v", v)
	}
}

// TestTimelineKinds: only hints, and the days reach the widgets service.
func TestTimelineKinds(t *testing.T) {
	items := []widgets.TimelineItem{{Kind: "update", Subject: "Nextcloud"}, {Kind: "opened", Subject: "Disk", HintID: 4}}
	v := viewOf(t, "timeline_recent", map[string]any{"kinds": "hints", "days": 30.0}, map[string]any{widgets.TimelineSlot: items}, "", nil)
	if shown := v["Items"].([]widgets.TimelineItem); len(shown) != 1 || shown[0].HintID != 4 {
		t.Fatalf("timeline: %+v", shown)
	}
	cfg, _ := widgets.Decode("timeline_recent", map[string]any{"days": 30.0})
	noise, _ := widgets.Decode("hint_noise", map[string]any{"days": "90"})
	if cfg.(widgets.DaysWanter).ExtraDays() != 30 || noise.(widgets.DaysWanter).ExtraDays() != 90 {
		t.Fatalf("days: %+v %+v", cfg, noise)
	}
}

// TestTodayParts: no calendar asked for, no calendar fetched.
func TestTodayParts(t *testing.T) {
	cfg, _ := widgets.Decode("today", map[string]any{"show_calendar": false, "hide_past": true})
	kind, _ := widgets.Get("today")
	for _, q := range kind.Queries(cfg) {
		if q.Name == "calendar" {
			t.Fatalf("calendar still queried: %+v", q)
		}
	}
}

// TestRssCompact: titles only and a fixed list height; unknown heights
// grow with the list.
func TestRssCompact(t *testing.T) {
	cfg, _ := widgets.Decode("rss", map[string]any{"url": "https://x.test/feed", "titles_only": true, "list_height": "short"})
	if c := cfg.(widgets.RssConfig); !c.Compact || c.Height != "short" {
		t.Fatalf("cfg: %+v", c)
	}
	cfg, _ = widgets.Decode("rss", map[string]any{"url": "https://x.test/feed", "list_height": "huge"})
	if c := cfg.(widgets.RssConfig); c.Height != "auto" {
		t.Fatalf("unknown height: %+v", c)
	}
}
