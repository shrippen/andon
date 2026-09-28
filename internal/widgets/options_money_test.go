package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

func viewOf(t *testing.T, key string, raw map[string]any, results map[string]any, service enums.ServiceType, settings map[string]any) map[string]any {
	t.Helper()
	kind, ok := widgets.Get(key)
	if !ok {
		t.Fatalf("%s not registered", key)
	}
	cfg, _ := widgets.Decode(key, raw)
	return kind.View(cfg, results, ctxFor(service, settings))
}

// TestChartOptions: last year can go, values show on the bars, and the
// revenue goal draws a line per month.
func TestChartOptions(t *testing.T) {
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 1, Status: "paid", Date: "2026-08-10", Net: 6000}}}
	goal := map[string]any{"goals": map[string]any{"revenue_year": 60000.0}}
	v := viewOf(t, "chart", map[string]any{"show_prev": false, "values": true, "goal_line": true}, map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, goal)
	if v["ShowPrev"] != false || v["Values"] != true || v["GoalY"] == nil {
		t.Fatalf("chart: %+v", v)
	}
}

// TestTrendOptions: a target line and a 7-day smoothing.
func TestTrendOptions(t *testing.T) {
	var points [][2]any
	for i, v := range []float64{10, 30, 10, 30, 10, 30, 10, 30, 10} {
		points = append(points, [2]any{"2026-09-0" + string(rune('1'+i)), v})
	}
	v := viewOf(t, "trend", map[string]any{"target_value": 20.0, "smooth": true}, map[string]any{"points": points}, "", nil)
	if v["TargetY"] == nil || v["Smooth"] != true {
		t.Fatalf("trend: %+v", v)
	}
	if v["High"].(float64)-v["Low"].(float64) >= 20 {
		t.Fatalf("not smoothed: %v..%v", v["Low"], v["High"])
	}
}

// TestProgressOptions: only named projects, no soll mark, own warning.
func TestProgressOptions(t *testing.T) {
	kimai := &sources.KimaiDataset{
		Projects: []sources.KimaiProject{
			{ID: 1, Name: "Relaunch", TimeBudgetMin: 600, BudgetType: "month"},
			{ID: 2, Name: "Portal", TimeBudgetMin: 600, BudgetType: "month"}},
		Timesheets: []sources.KimaiSheet{{ProjectID: 1, Begin: "2026-09-02", Minutes: 360}},
	}
	v := viewOf(t, "progress", map[string]any{"projects": []any{"relaunch"}, "soll": false, "warn_ahead": 20.0}, map[string]any{"data": kimai}, enums.ServiceKimai, nil)
	items := v["Items"].([]widgets.ProgressItem)
	if len(items) != 1 || items[0].Soll != 0 || items[0].Tier != "green" {
		t.Fatalf("progress: %+v", items)
	}
}

// TestDeadlineOptions: kinds can be left out, amounts hidden.
func TestDeadlineOptions(t *testing.T) {
	tax := map[string]any{"tax": map[string]any{"vat": map[string]any{"return_interval": "monthly"}, "prepayments": map[string]any{"amount": 1200.0}}}
	v := viewOf(t, "deadlines", map[string]any{"days": 120.0, "show_vat": false, "amounts": false}, nil, "", tax)
	for _, it := range v["Items"].([]map[string]any) {
		if it["Kind"] == "vat_return" || it["Amount"] != nil {
			t.Fatalf("item: %+v", it)
		}
	}
}

// TestCashflowOptions: a minimum balance and later payments.
func TestCashflowOptions(t *testing.T) {
	ninja := &sources.NinjaDataset{Currency: "EUR", Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Number: "R1", Status: "sent", Date: "2026-09-10", DueDate: "2026-09-24", Balance: 1000}}}
	on := viewOf(t, "cashflow", map[string]any{"min_balance": 500.0, "delay": 30.0}, map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, nil)
	off := viewOf(t, "cashflow", map[string]any{}, map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, nil)
	if on["MinY"] == nil || on["BelowMin"] != true {
		t.Fatalf("minimum: %+v", on)
	}
	if on["Path"] == off["Path"] {
		t.Fatal("delay did not move the payment")
	}
}

// TestHeatmapOptions: fewer months, no weekends, colour by daily goal.
func TestHeatmapOptions(t *testing.T) {
	// The daily goal is the Kimai work contract's (8 h Monday to Friday).
	kimai := &sources.KimaiDataset{Contract: sources.DemoContract(),
		Timesheets: []sources.KimaiSheet{{Begin: "2026-09-14", Minutes: 480}, {Begin: "2026-09-13", Minutes: 60}}}
	v := viewOf(t, "heatmap", map[string]any{"months": 3.0, "weekdays": true, "by_goal": true}, map[string]any{"data": kimai}, enums.ServiceKimai, nil)
	cells := v["Cells"].([]widgets.HeatCell)
	if len(cells) > 5*14 {
		t.Fatalf("too many cells: %d", len(cells))
	}
	var met bool
	for _, c := range cells {
		if c.Day == "2026-09-13" {
			t.Fatal("sunday shown")
		}
		if c.Day == "2026-09-14" && c.Goal == "met" {
			met = true
		}
	}
	if !met {
		t.Fatalf("goal colouring: %+v", cells[len(cells)-2:])
	}
}

// TestKpiDetails: open and overdue amounts and unbilled work list what
// they are made of, so a click on the value answers "which ones?".
func TestKpiDetails(t *testing.T) {
	now := time.Now()
	ninja := viewOf(t, "kpi", map[string]any{"metric": "open_amount"}, map[string]any{"data": sources.DemoNinja(now)}, enums.ServiceInvoiceNinja, nil)
	if k := ninja["KPI"].(*widgets.KpiResult); len(k.Details) == 0 || k.Details[0].Amount == 0 {
		t.Fatalf("open amount details: %+v", k.Details)
	}
	kimai := viewOf(t, "kpi", map[string]any{"metric": "unbilled"}, map[string]any{"data": sources.DemoKimai(now)}, enums.ServiceKimai, nil)
	if k := kimai["KPI"].(*widgets.KpiResult); len(k.Details) == 0 || k.Details[0].Note == "" {
		t.Fatalf("unbilled details: %+v", k.Details)
	}
}

// TestCashOptions: the balance tile shows its last 30 days and, with
// Invoice Ninja, what is free to spend.
func TestCashOptions(t *testing.T) {
	now := time.Now()
	results := map[string]any{"data": sources.DemoSure(now), "invoiceninja": sources.DemoNinja(now)}
	v := viewOf(t, "kpi", map[string]any{"metric": "cash", "free": true}, results, enums.ServiceSure, nil)
	k := v["KPI"].(*widgets.KpiResult)
	if k.Spark == nil || k.SparkDays != 30 || k.SubKey != "kpi.free" {
		t.Fatalf("cash: %+v", k)
	}
}
