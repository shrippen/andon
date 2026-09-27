package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestRateTrend: revenue over tracked hours per month; the tile shows
// the last complete month against the one before, with a line over them.
func TestRateTrend(t *testing.T) {
	kind, ok := widgets.Get("rate_trend")
	if !ok {
		t.Fatal("rate_trend not registered")
	}
	cfg, _ := widgets.Decode("rate_trend", map[string]any{"target": 90.0})
	ninja := &sources.NinjaDataset{Currency: "EUR", Invoices: []sources.NinjaInvoice{
		{ID: 1, ClientID: 1, Status: "paid", Date: "2026-07-10", Net: 800},
		{ID: 2, ClientID: 1, Status: "paid", Date: "2026-08-10", Net: 1000},
	}}
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-07-03", Minutes: 600}, {Begin: "2026-08-03", Minutes: 600},
	}}
	view := kind.View(cfg, map[string]any{"data": ninja, "kimai": kimai}, ctxFor(enums.ServiceInvoiceNinja, nil))
	if view["Rate"] != 100.0 || view["Prev"] != 80.0 || view["Target"] != 90.0 || view["Spark"] == nil {
		t.Fatalf("view: %+v", view)
	}
}
