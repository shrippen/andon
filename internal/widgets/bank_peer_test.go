package widgets

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestBankPeersReadFirefly: tiles with Sure as partner query Firefly III
// too, and read whichever the space has (metrics.BankOf), Sure first.
func TestBankPeersReadFirefly(t *testing.T) {
	for _, key := range []string{"cashflow", "subscriptions", "homelab_cost"} {
		kind, _ := Get(key)
		cfg, _ := Decode(key, map[string]any{})
		services := map[enums.ServiceType]bool{}
		for _, q := range kind.Queries(cfg) {
			services[q.Service] = true
		}
		if !services[enums.ServiceSure] || !services[enums.ServiceFirefly] {
			t.Errorf("%s queries %v", key, services)
		}
	}
	cfg, _ := Decode("kpi", map[string]any{"metric": "liquidity_30"})
	kind, _ := Get("kpi")
	services := map[enums.ServiceType]bool{}
	for _, q := range kind.Queries(cfg) {
		services[q.Service] = true
	}
	if !services[enums.ServiceFirefly] {
		t.Errorf("kpi liquidity queries %v", services)
	}

	// Liquidity with Firefly's recurring payments in Sure's place.
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{{Status: "sent", Date: "2026-09-01", DueDate: "2026-10-01", Balance: 1000, Net: 840}}}
	firefly := &sources.SureDataset{Recurring: []sources.SureRecurring{{Name: "Miete", Status: "active", Amount: 450, Expense: true, Next: "2026-10-01"}}}
	ctx := ViewCtx{Today: "2026-09-20", Service: string(enums.ServiceInvoiceNinja)}
	view := kind.View(cfg, map[string]any{"data": ninja, "firefly": firefly}, ctx)
	if kpi := view["KPI"].(*KpiResult); kpi.SubOut != 450 {
		t.Fatalf("kpi: %+v", kpi)
	}
}

// TestCashflowDepotLine: the depot is no cash: it stays out of the
// forecast and shows as a line of its own in the dialog.
func TestCashflowDepotLine(t *testing.T) {
	now := time.Now().UTC()
	ninja := sources.DemoNinja(now)
	results := map[string]any{"data": ninja, "ghostfolio": &sources.GhostfolioDataset{Currency: "EUR", Value: 12000}}
	ctx := ViewCtx{Today: now.Format(time.DateOnly), Service: string(enums.ServiceInvoiceNinja)}
	with := cashflowDetail(CashflowConfig{Days: 60}, results, ctx).Body.(*DetailBody)
	delete(results, "ghostfolio")
	without := cashflowDetail(CashflowConfig{Days: 60}, results, ctx).Body.(*DetailBody)

	if len(with.Line) != len(without.Line)+1 || with.Line[len(with.Line)-1].Label.Key != "detail.cash.depot" {
		t.Fatalf("line: %+v", with.Line)
	}
	if with.Facts[0].Value.(map[string]any)["$money"] != without.Facts[0].Value.(map[string]any)["$money"] {
		t.Fatal("the depot changed the cash")
	}
}
