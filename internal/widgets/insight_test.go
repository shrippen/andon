package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

func ctxFor(service enums.ServiceType, settings map[string]any) widgets.ViewCtx {
	if settings == nil {
		settings = map[string]any{}
	}
	return widgets.ViewCtx{Today: "2026-09-15", Settings: settings, Service: string(service)}
}

func TestDecodeKpiDefaultsToRevenueYTD(t *testing.T) {
	cfg, _ := widgets.Decode("kpi", map[string]any{})
	kpi := cfg.(widgets.KpiConfig)
	if kpi.Metric != widgets.MetricRevenueYTD {
		t.Fatalf("expected default metric revenue_ytd, got %s", kpi.Metric)
	}
}

func TestDecodeTableLimitClamped(t *testing.T) {
	cfg, _ := widgets.Decode("table", map[string]any{"limit": float64(999)})
	table := cfg.(widgets.TableConfig)
	if table.Limit != 50 {
		t.Fatalf("expected limit clamped to 50, got %d", table.Limit)
	}
}

func TestKpiViewKimaiHoursToday(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "hours_today"})
	data := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-09-15", Minutes: 90, Billable: true},
		{Begin: "2026-09-14", Minutes: 999, Billable: true}, // a different day must not count
	}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceKimai, nil))
	kpi, ok := view["KPI"].(*widgets.KpiResult)
	if !ok || kpi == nil {
		t.Fatalf("expected a KPI result, got %+v", view)
	}
	if kpi.Kind != "hours" || kpi.Value != 1.5 {
		t.Fatalf("expected 1.5 hours today, got %+v", kpi)
	}
}

func TestKpiViewNinjaOpenAmountHasInvoiceCount(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "open_amount"})
	data := &sources.NinjaDataset{Currency: "EUR", Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "sent", Date: "2026-08-01", DueDate: "2026-08-15", Balance: 500},
	}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, nil))
	kpi := view["KPI"].(*widgets.KpiResult)
	if kpi.Kind != "money" || kpi.Value != 500 || kpi.SubKey != "kpi.invoices" || kpi.SubCount != 1 {
		t.Fatalf("unexpected kpi: %+v", kpi)
	}
}

func TestKpiViewMissingDataIsEmptyNotUnsupported(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{})
	view := kind.View(cfg, map[string]any{}, ctxFor(enums.ServiceInvoiceNinja, nil))
	if view["KPI"] != nil || view["Unsupported"] != nil {
		t.Fatalf("expected an empty view with no connection yet, got %+v", view)
	}
}

func TestKpiViewUnknownServiceIsUnsupported(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "revenue_ytd"})
	view := kind.View(cfg, map[string]any{"data": &sources.SnipeDataset{}}, ctxFor(enums.ServiceSnipeIT, nil))
	if view["Unsupported"] != true {
		t.Fatalf("expected revenue_ytd to be unsupported for snipeit, got %+v", view)
	}
}

func TestTableViewOpenInvoices(t *testing.T) {
	kind, _ := widgets.Get("table")
	cfg, _ := widgets.Decode("table", map[string]any{"table": "open_invoices"})
	data := &sources.NinjaDataset{Clients: []sources.NinjaClient{{ID: 1, Name: "Acme"}}, Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "sent", Date: "2026-08-01", DueDate: "2026-08-01", Balance: 200},
	}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, nil))
	rows, ok := view["Rows"].([]widgets.Row)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected 1 row, got %+v", view)
	}
	if rows[0].Values[1] != "Acme" {
		t.Fatalf("expected client name in row, got %+v", rows[0].Values)
	}
}

func TestTableViewUnsupportedCombination(t *testing.T) {
	kind, _ := widgets.Get("table")
	cfg, _ := widgets.Decode("table", map[string]any{"table": "trips"})
	view := kind.View(cfg, map[string]any{"data": &sources.KimaiDataset{}}, ctxFor(enums.ServiceKimai, nil))
	if view["Unsupported"] != true {
		t.Fatalf("expected trips+kimai to be unsupported, got %+v", view)
	}
}

func TestChartViewHoursMonthsMatchesConfig(t *testing.T) {
	kind, _ := widgets.Get("chart")
	cfg, _ := widgets.Decode("chart", map[string]any{"chart": "hours", "months": float64(3)})
	view := kind.View(cfg, map[string]any{"data": &sources.KimaiDataset{}}, ctxFor(enums.ServiceKimai, nil))
	bars, ok := view["Bars"].([]widgets.Bar)
	if !ok || len(bars) != 3 {
		t.Fatalf("expected 3 bars, got %+v", view)
	}
}

func TestDeadlinesViewUnconfiguredHasNoItems(t *testing.T) {
	kind, _ := widgets.Get("deadlines")
	cfg, _ := widgets.Decode("deadlines", map[string]any{})
	view := kind.View(cfg, nil, ctxFor("", nil))
	if view["Configured"] != false {
		t.Fatalf("expected unconfigured deadlines, got %+v", view)
	}
}

func TestProgressViewRevenueGoal(t *testing.T) {
	kind, _ := widgets.Get("progress")
	cfg, _ := widgets.Decode("progress", map[string]any{})
	settings := map[string]any{"goals": map[string]any{"revenue_year": 10000.0}}
	data := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "paid", Date: "2026-01-10", Net: 2500},
	}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, settings))
	items, ok := view["Items"].([]widgets.ProgressItem)
	if !ok || len(items) != 1 || items[0].Pct != 0.25 {
		t.Fatalf("expected 25%% progress towards goal, got %+v", view)
	}
}

func TestKpiLiquiditySubtractsFixedCosts(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "liquidity_30"})
	data := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Status: "sent", Date: "2026-09-01", DueDate: "2026-10-01", Balance: 1000, Net: 840},
	}}
	settings := map[string]any{"costs": map[string]any{"fixed_monthly": 300.0}}

	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, settings))
	kpi := view["KPI"].(*widgets.KpiResult)
	if kpi.Value != 700 || kpi.SubOut != 300 {
		t.Fatalf("unexpected: %+v", kpi)
	}
}

func TestKpiLiquidityUsesSureRecurring(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "liquidity_30"})
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Status: "sent", Date: "2026-09-01", DueDate: "2026-10-01", Balance: 1000, Net: 840},
	}}
	sure := &sources.SureDataset{Recurring: []sources.SureRecurring{
		{Name: "Miete", Status: "active", Amount: 450, Expense: true, Next: "2026-10-01"},
		{Name: "Gehalt", Status: "active", Amount: 3000, Next: "2026-10-01"},
		{Name: "Jahresbeitrag", Status: "active", Amount: 99, Expense: true, Next: "2027-01-01"},
	}}
	settings := map[string]any{"costs": map[string]any{"fixed_monthly": 300.0}}

	view := kind.View(cfg, map[string]any{"data": ninja, "sure": sure}, ctxFor(enums.ServiceInvoiceNinja, settings))
	if kpi := view["KPI"].(*widgets.KpiResult); kpi.Value != 550 || kpi.SubOut != 450 {
		t.Fatalf("kpi: %+v", kpi)
	}
}

// TestProgressTiers: an exceeded revenue goal is good (green, with the
// overshoot), one behind the calendar is yellow; a monthly budget used
// faster than the month passes is yellow, a spent one red. The 15 Sep
// mark sits at 258/365 of the year and 15/30 of the month.
func TestProgressTiers(t *testing.T) {
	kind, _ := widgets.Get("progress")
	cfg, _ := widgets.Decode("progress", map[string]any{})
	goal := map[string]any{"goals": map[string]any{"revenue_year": 10000.0}}
	item := func(view map[string]any) widgets.ProgressItem {
		items := view["Items"].([]widgets.ProgressItem)
		if len(items) != 1 {
			t.Fatalf("items: %+v", items)
		}
		return items[0]
	}

	ahead := item(kind.View(cfg, map[string]any{"data": &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "paid", Date: "2026-01-10", Net: 12500}}}}, ctxFor(enums.ServiceInvoiceNinja, goal)))
	if ahead.Tier != "green" || ahead.Fill != 80 || ahead.Over != 20 {
		t.Fatalf("exceeded goal: %+v", ahead)
	}
	behind := item(kind.View(cfg, map[string]any{"data": &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "paid", Date: "2026-01-10", Net: 2500}}}}, ctxFor(enums.ServiceInvoiceNinja, goal)))
	if behind.Tier != "yellow" || behind.Soll < 70 || behind.Soll > 71 {
		t.Fatalf("goal behind the calendar: %+v", behind)
	}

	budget := func(usedMin int) widgets.ProgressItem {
		data := &sources.KimaiDataset{
			Projects:   []sources.KimaiProject{{ID: 1, Name: "Relaunch", TimeBudgetMin: 600, BudgetType: "month"}},
			Timesheets: []sources.KimaiSheet{{ProjectID: 1, Begin: "2026-09-02", Minutes: usedMin}},
		}
		return item(kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceKimai, nil)))
	}
	if fast := budget(480); fast.Tier != "yellow" || fast.Soll != 50 {
		t.Fatalf("budget used too fast: %+v", fast)
	}
	if calm := budget(240); calm.Tier != "green" {
		t.Fatalf("budget on pace: %+v", calm)
	}
	if spent := budget(660); spent.Tier != "red" {
		t.Fatalf("spent budget: %+v", spent)
	}
}

// TestKpiOptions: the comparison can be turned off or switched to the
// month before, a target colours the value (lower is better for open
// amounts), and the line can be left out.
func TestKpiOptions(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	data := &sources.NinjaDataset{Currency: "EUR", Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "paid", Date: "2026-08-10", Net: 1000},
		{ID: 2, ClientID: 1, Status: "paid", Date: "2026-09-05", Net: 1500},
		{ID: 3, ClientID: 1, Status: "sent", Date: "2026-09-01", DueDate: "2026-09-30", Balance: 800},
	}}
	view := func(raw map[string]any) *widgets.KpiResult {
		cfg, _ := widgets.Decode("kpi", raw)
		return kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, nil))["KPI"].(*widgets.KpiResult)
	}
	if k := view(map[string]any{"metric": "revenue_month", "compare": "prev_month"}); !k.HasDelta || k.Delta != 0.5 || k.DeltaKey != "kpi.vs_last_month" {
		t.Fatalf("month compare: %+v", k)
	}
	if k := view(map[string]any{"metric": "revenue_ytd", "compare": "off"}); k.HasDelta {
		t.Fatalf("compare off: %+v", k)
	}
	if k := view(map[string]any{"metric": "revenue_month", "target_value": 1000.0}); k.Target != "good" {
		t.Fatalf("target met: %+v", k)
	}
	if k := view(map[string]any{"metric": "open_amount", "target_value": 500.0}); k.Target != "bad" {
		t.Fatalf("open amount above its target: %+v", k)
	}
	if k := view(map[string]any{"metric": "revenue_ytd", "spark": false}); k.Spark != nil {
		t.Fatalf("line switched off: %+v", k)
	}
}

// An untitled KPI tile's dialog is named after its metric, as the tile
// reads, not the generic type name "Key figure".
func TestKpiDetailNamedByMetric(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "hours_today"})
	data := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{{Begin: "2026-09-15", Minutes: 90}}}
	view := kind.Detail(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceKimai, nil))
	if view.Head.TitleKey != "opt.hours_today" {
		t.Fatalf("title key = %q, want opt.hours_today", view.Head.TitleKey)
	}
}
