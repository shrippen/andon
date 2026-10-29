package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestZM: services to EU businesses at 0 % sum per VAT ID over the
// quarter, by invoice date; a client without one is named, not summed.
func TestZM(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276",
		Clients: []sources.NinjaClient{{ID: 1, CountryID: "276"}, {ID: 2, Name: "Atelier Lyon", CountryID: "250", VATNumber: "fr 12 345678901"},
			{ID: 3, Name: "Studio Wien", CountryID: "40", VATNumber: "ATU12345678"}, {ID: 4, Name: "Agentur Gent", CountryID: "56"},
			{ID: 5, Name: "Studio Graz", CountryID: "40", VATNumber: "ATU12345678"}},
		Invoices: []sources.NinjaInvoice{
			{ClientID: 1, Status: "sent", Date: "2026-08-03", Net: 1000, Taxes: 190},
			{ClientID: 2, Status: "sent", Date: "2026-07-05", Net: 500.9},
			{ClientID: 2, Status: "sent", Date: "2026-09-30", Net: 250},
			{ClientID: 3, Status: "sent", Date: "2026-08-15", Net: 300},
			{ClientID: 5, Status: "sent", Date: "2026-08-16", Net: 100},
			{ClientID: 4, Status: "sent", Date: "2026-08-20", Net: 80},
			{ClientID: 2, Status: "sent", Date: "2026-10-01", Net: 999},
			{ClientID: 2, Status: "draft", Date: "2026-08-01", Net: 777},
		},
	}
	zm := metrics.ZMOf(n, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC))

	if zm.Key != "2026-Q3" || zm.Due.Format(time.DateOnly) != "2026-10-25" {
		t.Errorf("period: %s due %s", zm.Key, zm.Due.Format(time.DateOnly))
	}
	want := []metrics.ZMLine{{Country: "AT", ID: "U12345678", Amount: 400, Clients: "Studio Graz, Studio Wien"},
		{Country: "FR", ID: "12345678901", Amount: 750, Clients: "Atelier Lyon"}}
	if len(zm.Lines) != len(want) {
		t.Fatalf("lines: %+v", zm.Lines)
	}
	for i, w := range want {
		if zm.Lines[i] != w {
			t.Errorf("line %d: %+v, want %+v", i, zm.Lines[i], w)
		}
	}
	if len(zm.NoID) != 1 || zm.NoID[0] != "Agentur Gent" {
		t.Errorf("without VAT ID: %v", zm.NoID)
	}
}

// TestZMSkipsHome: a client in the company's own country is no EU
// service, also when the company is not in Germany.
func TestZMSkipsHome(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "40",
		Clients:  []sources.NinjaClient{{ID: 1, Name: "Studio Wien", CountryID: "40", VATNumber: "ATU12345678"}},
		Invoices: []sources.NinjaInvoice{{ClientID: 1, Status: "sent", Date: "2026-08-15", Net: 300}},
	}
	if zm := metrics.ZMOf(n, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)); len(zm.Lines) != 0 || len(zm.NoID) != 0 {
		t.Errorf("home client reported: %+v", zm)
	}
}
