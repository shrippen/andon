package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

func TestEffectiveRates(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Muster GmbH"}, {ID: 2, Name: "Nur Kimai"}},
		Timesheets: []sources.KimaiSheet{
			{Begin: "2026-09-01T09:00:00Z", Minutes: 600, CustomerID: 1},
			{Begin: "2026-08-01T09:00:00Z", Minutes: 600, CustomerID: 1},
			{Begin: "2024-01-01T09:00:00Z", Minutes: 6000, CustomerID: 1},
			{Begin: "2026-09-02T09:00:00Z", Minutes: 60, CustomerID: 2},
		},
	}
	ninja := &sources.NinjaDataset{
		Clients: []sources.NinjaClient{{ID: 7, Name: "muster gmbh "}},
		Invoices: []sources.NinjaInvoice{
			{ClientID: 7, Status: "paid", Date: "2026-09-10", Net: 1500},
			{ClientID: 7, Status: "draft", Date: "2026-09-11", Net: 9999},
		},
	}
	rows, overall := EffectiveRates(kimai, ninja, today, nil)
	if len(rows) != 1 || rows[0].Hours != 20 || rows[0].Net != 1500 || rows[0].Rate != 75 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if overall != 75 {
		t.Fatalf("overall rate: %v", overall)
	}
}

// A stored link joins different names; "no counterpart" drops a same
// name.
func TestEffectiveRatesFollowLinks(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	kimai := &sources.KimaiDataset{
		Customers:  []sources.KimaiCustomer{{ID: 1, Name: "Muster"}, {ID: 2, Name: "Beta"}},
		Timesheets: []sources.KimaiSheet{{Begin: "2026-09-01T09:00:00Z", Minutes: 600, CustomerID: 1}, {Begin: "2026-09-01T09:00:00Z", Minutes: 600, CustomerID: 2}},
	}
	ninja := &sources.NinjaDataset{
		Clients:  []sources.NinjaClient{{ID: 7, Key: "Kx9", Name: "Muster Holding AG"}, {ID: 8, Key: "Zz1", Name: "Beta"}},
		Invoices: []sources.NinjaInvoice{{ClientID: 7, Status: "paid", Date: "2026-09-10", Net: 1000}, {ClientID: 8, Status: "paid", Date: "2026-09-10", Net: 500}},
	}
	rows, _ := EffectiveRates(kimai, ninja, today, nil)
	if len(rows) != 1 || rows[0].Customer != "Beta" {
		t.Fatalf("by name: %+v", rows)
	}
	rows, _ = EffectiveRates(kimai, ninja, today, ClientMap{1: "Kx9", 2: ""})
	if len(rows) != 1 || rows[0].Customer != "Muster Holding AG" || rows[0].Rate != 100 {
		t.Fatalf("linked: %+v", rows)
	}
}
