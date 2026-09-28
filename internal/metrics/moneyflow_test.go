package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Money on its way from work to account: unbilled hours, drafts, sent
// and not yet due, overdue, paid in the last 30 days.
func TestMoneyFlow(t *testing.T) {
	today := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Billable: true, Begin: "2026-09-20T09:00:00", End: "2026-09-20T12:00:00", Rate: 300, CustomerID: 1},
		{Billable: true, Exported: true, Begin: "2026-09-01T09:00:00", End: "2026-09-01T12:00:00", Rate: 300, CustomerID: 1},
	}}
	ninja := &sources.NinjaDataset{Currency: "EUR",
		Invoices: []sources.NinjaInvoice{
			{Status: "draft", Amount: 500, Balance: 500},
			{Status: "sent", DueDate: "2026-10-10", Amount: 1000, Balance: 1000},
			{Status: "sent", DueDate: "2026-09-10", Amount: 800, Balance: 800},
			{Status: "paid", Amount: 400},
		},
		Payments: []sources.NinjaPayment{{Date: "2026-09-15", Amount: 400}, {Date: "2026-07-01", Amount: 900}},
	}
	f := metrics.MoneyFlowOf(kimai, ninja, today)
	if f.Unbilled != 300 || f.Drafts != 500 || f.Sent != 1000 || f.Overdue != 800 || f.Paid != 400 || f.Currency != "EUR" {
		t.Fatalf("flow: %+v", f)
	}
	if only := metrics.MoneyFlowOf(nil, ninja, today); only.Unbilled != 0 || only.Sent != 1000 {
		t.Fatalf("without kimai: %+v", only)
	}
}
