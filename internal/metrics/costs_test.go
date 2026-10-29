package metrics_test

import (
	"math"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func TestHomelabCostAndShares(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	settings := metrics.HomelabSettingsOf(map[string]any{"homelab": map[string]any{"power_entity": "sensor.server",
		"power_price": 0.30, "hardware_years": 5.0, "domain_yearly": 12.0, "hosting_words": "hetzner"}})
	in := metrics.CostInputs{
		Hass:    &sources.HassDataset{Entities: []sources.Entity{{ID: "sensor.server", State: "50", Unit: "W"}}},
		Assets:  &sources.InventoryData{Assets: []sources.Asset{{Name: "NAS", PurchaseDate: "2024-01-01", PurchaseCost: 1200}, {Name: "Alt", PurchaseDate: "2015-01-01", PurchaseCost: 900}}},
		Domains: &sources.DomainsDataset{Domains: []sources.DomainInfo{{Name: "a.de"}, {Name: "b.org"}}},
		Sure:    &sources.BankData{Recurring: []sources.BankRecurring{{Name: "Hetzner Storage Box", Status: "active", Expense: true, Amount: 3.81}}},
	}
	bill := metrics.HomelabCost(in, settings, today)
	// power 50 W × 730 h × 0.30 = 10.95; NAS 1200/60 = 20; domains 2; hosting 3.81
	if len(bill.Items) != 4 || bill.Total != 36.76 {
		t.Fatalf("bill: %+v", bill)
	}

	// PV covered 60 % of the last 30 days' use: power costs 40 %,
	// 10.95 × 0.4 = 4.38.
	in.Solar = &sources.HassEnergy{Solar: []string{"pv"}, Import: []string{"in"}, Export: []string{"out"},
		Days: []sources.SolarDay{{Day: "2026-09-20", Solar: 10, Export: 4, Import: 4}}}
	if bill := metrics.HomelabCost(in, settings, today); bill.Items[0].Monthly != 4.38 {
		t.Fatalf("with PV: %+v", bill.Items[0])
	}
	in.Solar = nil

	proxmox := &sources.ProxmoxDataset{Guests: []sources.ProxmoxGuest{{Name: "docker", Running: true, CPU: 3}, {Name: "ha", Running: true, CPU: 1}}}
	split := metrics.PowerPerService(proxmox, 40)
	if len(split) != 2 || split[0].Name != "docker" || split[0].Monthly != 30 {
		t.Fatalf("split: %+v", split)
	}

	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{Name: "Acme GmbH"}}}
	share, n, all := metrics.BusinessShare(kimai, []string{"acme-shop", "immich", "dotfiles", "acme-api"})
	if n != 2 || all != 4 || share != 0.5 {
		t.Fatalf("share: %v %d %d", share, n, all)
	}
}

func TestWeekStory(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Beta"}},
		Timesheets: []sources.KimaiSheet{{Begin: "2026-09-22T09:00:00Z", Minutes: 360, CustomerID: 1}, {Begin: "2026-09-23T09:00:00Z", Minutes: 240, CustomerID: 2}}}
	ninja := &sources.NinjaDataset{Currency: "EUR", Payments: []sources.NinjaPayment{{Date: "2026-09-24", Amount: 1190}}}
	h := &metrics.History{Series: map[string][]metrics.Point{"truenas.pool.tank.used": {
		{Day: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), Value: 0.78}, {Day: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), Value: 0.81}}}}
	lines := metrics.StorySince(map[string]any{"kimai": kimai, "invoiceninja": ninja}, h, metrics.Today(now).AddDate(0, 0, -7), now)
	if len(lines) != 3 || lines[0].Key != "hours" || lines[0].Params["customer"] != "Acme" || lines[1].Key != "paid" || lines[2].Key != "storage" {
		t.Fatalf("lines: %+v", lines)
	}
}

// TestPowerDays: a day's runs give its mean draw and its cost at the
// hours' prices.
func TestPowerDays(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	day := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	h := &metrics.History{Series: map[string][]metrics.Point{
		"homelab.power.runs":  {{Day: day, Value: 2}},
		"homelab.power.watts": {{Day: day, Value: 200}},       // 100 W mean
		"homelab.power.cost":  {{Day: day, Value: 200 * 0.3}}, // at 0,30 €/kWh
	}}
	got := metrics.PowerDays(h, now, 3)
	if len(got) != 1 || got[0].KWh != 2.4 || math.Abs(got[0].Cost-0.72) > 1e-9 {
		t.Fatalf("days: %+v", got)
	}
}

// A server's power goes to its stacks, the rest evenly to all.
func TestCostPerStack(t *testing.T) {
	komodo := &sources.KomodoDataset{Stacks: []sources.KStack{{Name: "immich", Server: "regis"}, {Name: "kimai", Server: "regis"}, {Name: "blog", Server: "vps"}}}
	power := []metrics.ServiceCost{{Name: "Regis", Monthly: 30}}
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{Name: "Blog GmbH"}}}
	got := metrics.CostPerStack(metrics.HomelabBill{Total: 120}, power, komodo, kimai)
	if len(got) != 3 || got[0].Stack != "immich" || got[0].Power != 15 || got[0].Rest != 30 || got[0].Monthly != 45 {
		t.Fatalf("stacks: %+v", got)
	}
	if blog := got[2]; blog.Power != 0 || blog.Monthly != 30 || !blog.Business {
		t.Errorf("blog: %+v", blog)
	}
}

// TestRecordPowerSolar: each run's power cost is the grid price times
// the share PV left to the grid: 50 W × 0,30 € × 40 % = 6.
func TestRecordPowerSolar(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	settings := map[string]any{"homelab": map[string]any{"power_entity": "sensor.server", "power_price": 0.30}}
	data := map[string]any{
		"homeassistant": &sources.HassDataset{Entities: []sources.Entity{{ID: "sensor.server", State: "50", Unit: "W"}}},
		sources.HassEnergyKey: &sources.HassEnergy{Solar: []string{"pv"}, Import: []string{"in"}, Export: []string{"out"},
			Days: []sources.SolarDay{{Day: "2026-09-20", Solar: 10, Export: 4, Import: 4}}},
	}
	read := metrics.Read(metrics.Scope{Datasets: data, Settings: settings}, now)
	if got := read.Counts["homelab.power.cost"]; math.Abs(got-6) > 1e-9 || read.Counts["homelab.power.watts"] != 50 {
		t.Fatalf("counts: %v", read.Counts)
	}
}
