package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// A customer card joins Kimai and Invoice Ninja by name: hours this year
// and month, unbilled work, open and overdue invoices, revenue.
func TestClientCards(t *testing.T) {
	today := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Acme GmbH"}, {ID: 2, Name: "Solo"}},
		Projects:  []sources.KimaiProject{{ID: 10, Name: "Relaunch", CustomerID: 1}},
		Timesheets: []sources.KimaiSheet{
			{CustomerID: 1, ProjectID: 10, Begin: "2026-09-10T09:00:00", End: "x", Minutes: 120, Rate: 200, Billable: true},
			{CustomerID: 1, ProjectID: 10, Begin: "2026-02-10T09:00:00", End: "x", Minutes: 60, Rate: 100, Billable: true, Exported: true},
			{CustomerID: 1, ProjectID: 10, Begin: "2025-12-10T09:00:00", End: "x", Minutes: 600, Rate: 1000, Billable: true, Exported: true},
		},
	}
	ninja := &sources.NinjaDataset{Currency: "EUR",
		Clients: []sources.NinjaClient{{ID: 7, Name: "acme gmbh"}},
		Invoices: []sources.NinjaInvoice{
			{ClientID: 7, Number: "R-1", Status: "sent", Date: "2026-08-01", DueDate: "2026-08-15", Amount: 1190, Balance: 1190, Net: 1000},
			{ClientID: 7, Number: "R-0", Status: "paid", Date: "2026-03-01", Amount: 595, Net: 500},
		},
	}
	cards := metrics.ClientCards(kimai, ninja, today, metrics.CenterMean, nil, metrics.PeriodYear.Span(today))
	if len(cards) != 2 {
		t.Fatalf("cards: %+v", cards)
	}
	acme := cards[0]
	if acme.Name != "Acme GmbH" || acme.Hours != 3 || acme.Unbilled != 200 ||
		acme.Overdue != 1190 || acme.Open != 1190 || acme.Revenue != 1500 || len(acme.Invoices) != 1 || acme.Projects[0].Name != "Relaunch" {
		t.Fatalf("acme: %+v", acme)
	}
	if cards[1].Name != "Solo" || cards[1].Matched {
		t.Fatalf("solo: %+v", cards[1])
	}
}
