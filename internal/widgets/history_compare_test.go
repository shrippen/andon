package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// revenueSince is a dataset whose invoices begin in the month of first
// (one 1000 € invoice a month up to September 2026).
func revenueSince(first string) *sources.NinjaDataset {
	data := &sources.NinjaDataset{Currency: "EUR"}
	for _, m := range []string{"2024-10", "2024-11", "2024-12", "2025-01", "2025-02", "2025-03", "2025-04", "2025-05", "2025-06", "2025-07", "2025-08", "2025-09",
		"2025-10", "2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09"} {
		if m >= first {
			data.Invoices = append(data.Invoices, sources.NinjaInvoice{Status: "paid", Date: m + "-05", Net: 1000})
		}
	}
	return data
}

// lineFacts are a dialog's line facts by label key.
func lineFacts(t *testing.T, d widgets.DetailView) map[string]any {
	t.Helper()
	body, ok := d.Body.(*widgets.DetailBody)
	if !ok {
		t.Fatalf("body: %+v", d.Body)
	}
	out := map[string]any{}
	for _, f := range body.Line {
		out[f.Label.Key] = f.Value
	}
	return out
}

// TestChartCompareNeedsHistory: the year-over-year sum and change only
// show when the data reaches back over the compared months; otherwise
// the dialog says where the data begins.
func TestChartCompareNeedsHistory(t *testing.T) {
	kind, _ := widgets.Get("chart")
	cfg, _ := widgets.Decode("chart", map[string]any{"chart": "revenue", "months": 12})
	ctx := ctxFor(enums.ServiceInvoiceNinja, nil)

	full := lineFacts(t, kind.Detail(cfg, map[string]any{"data": revenueSince("2024-01")}, ctx))
	if _, ok := full["detail.chart.change"]; !ok {
		t.Fatalf("full history without change: %+v", full)
	}
	short := lineFacts(t, kind.Detail(cfg, map[string]any{"data": revenueSince("2025-02")}, ctx))
	if _, ok := short["detail.chart.change"]; ok {
		t.Fatalf("change over incomplete history: %+v", short)
	}
	if _, ok := short["chart.prev"]; ok {
		t.Fatalf("last year's sum over incomplete history: %+v", short)
	}
	if _, ok := short["period.data_from_label"]; !ok {
		t.Fatalf("no data start named: %+v", short)
	}
}

// TestRevenueYTDCompareNeedsHistory: the revenue KPI compares with last
// year only when last year's data starts on January 1st.
func TestRevenueYTDCompareNeedsHistory(t *testing.T) {
	cfg := map[string]any{"metric": "revenue_ytd"}
	full := viewOf(t, "kpi", cfg, map[string]any{"data": revenueSince("2024-01")}, enums.ServiceInvoiceNinja, nil)["KPI"].(*widgets.KpiResult)
	if !full.HasDelta {
		t.Fatalf("full history without delta: %+v", full)
	}
	short := viewOf(t, "kpi", cfg, map[string]any{"data": revenueSince("2025-02")}, enums.ServiceInvoiceNinja, nil)["KPI"].(*widgets.KpiResult)
	if short.HasDelta || short.SubKey != "kpi.data_from" || short.SubStart != "02/2025" {
		t.Fatalf("delta over incomplete history: %+v", short)
	}
}

// TestKpiNamesPeriod: a KPI over a period names it under the value (today
// is 2026-09-15); amounts as of now say so.
func TestKpiNamesPeriod(t *testing.T) {
	ninja := map[string]any{"data": revenueSince("2024-01")}
	kimai := map[string]any{"data": &sources.KimaiDataset{}}
	for _, c := range []struct {
		metric  string
		results map[string]any
		service enums.ServiceType
		key     string
		arg     string
	}{
		{"revenue_ytd", ninja, enums.ServiceInvoiceNinja, "kpi.period_year", "2026"},
		{"revenue_month", ninja, enums.ServiceInvoiceNinja, "kpi.period_month", "09/2026"},
		{"revenue_forecast", ninja, enums.ServiceInvoiceNinja, "kpi.period_year", "2026"},
		{"open_amount", ninja, enums.ServiceInvoiceNinja, "period.today", ""},
		{"hours_month", kimai, enums.ServiceKimai, "kpi.period_month", "09/2026"},
		{"hours_week", kimai, enums.ServiceKimai, "kpi.period_week", "38"},
	} {
		k := viewOf(t, "kpi", map[string]any{"metric": c.metric}, c.results, c.service, nil)["KPI"].(*widgets.KpiResult)
		if k.PeriodKey != c.key || k.PeriodArg != c.arg {
			t.Errorf("%s: %q %q, want %q %q", c.metric, k.PeriodKey, k.PeriodArg, c.key, c.arg)
		}
	}
}

// TestClientSharesNamePeriod: the revenue-per-client table says it
// counts the last 12 months.
func TestClientSharesNamePeriod(t *testing.T) {
	v := viewOf(t, "table", map[string]any{"table": "client_shares"}, map[string]any{"data": revenueSince("2024-01")}, enums.ServiceInvoiceNinja, nil)
	if v["PeriodKey"] != "period.last_12m" {
		t.Fatalf("view: %+v", v)
	}
}

// TestSeasonNeedsHistory: the seasonal average over earlier years names
// where the data begins when it cannot cover three full years, and the
// dialog gives no change over it.
func TestSeasonNeedsHistory(t *testing.T) {
	kind, _ := widgets.Get("chart")
	cfg, _ := widgets.Decode("chart", map[string]any{"chart": "seasonal", "months": 12})
	ctx := ctxFor(enums.ServiceInvoiceNinja, nil)

	short := lineFacts(t, kind.Detail(cfg, map[string]any{"data": revenueSince("2025-02")}, ctx))
	if _, ok := short["period.data_from_label"]; !ok {
		t.Fatalf("no data start named: %+v", short)
	}
	if _, ok := short["detail.chart.change"]; ok {
		t.Fatalf("change over incomplete history: %+v", short)
	}
}
