package metrics_test

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/sources"
)

func TestElster(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276",
		Clients: []sources.NinjaClient{{ID: 1, CountryID: "276"}, {ID: 2, CountryID: "250"}},
		Invoices: []sources.NinjaInvoice{
			{ClientID: 1, Status: "sent", Date: "2026-09-03", Net: 1000.6, Taxes: 190.11, Amount: 1190.71},
			{ClientID: 1, Status: "sent", Date: "2026-09-04", Net: 100, Taxes: 7, Amount: 107},
			{ClientID: 2, Status: "sent", Date: "2026-09-05", Net: 500, Amount: 500},
			{ClientID: 1, Status: "sent", Date: "2026-10-01", Net: 999, Taxes: 189.81, Amount: 1188.81},
		},
		Vendors: []sources.NinjaVendor{{Key: "de", CountryID: "276"}, {Key: "ie", Name: "Cloud IE", CountryID: "372"}, {Key: "us", Name: "AI US", CountryID: "840"}},
		Expenses: []sources.NinjaExpense{
			{VendorKey: "de", Date: "2026-09-10", Amount: 119, Tax: 19},
			{VendorKey: "ie", Date: "2026-09-11", Amount: 100},
			{VendorKey: "us", Date: "2026-09-12", Amount: 50.5},
		},
	}
	start, _ := metrics.ParseDay("2026-09-01")
	end, _ := metrics.ParseDay("2026-09-30")
	e := metrics.ElsterOf(n, start, end, "soll")

	if e.Kz81 != 1000 || e.Kz86 != 100 || e.Kz21 != 500 || e.Kz45 != 0 {
		t.Errorf("revenue: 81 %v 86 %v 21 %v 45 %v", e.Kz81, e.Kz86, e.Kz21, e.Kz45)
	}
	if e.Kz46 != 100 || e.Kz47 != 19 || e.Kz84 != 50 || e.Kz85 != 9.5 || e.Kz66 != 19 || e.Kz67 != 28.5 || len(e.Reverse) != 2 {
		t.Errorf("input: %+v", e)
	}
	// 190 + 7 + 19 + 9.5 output, 19 + 28.5 input.
	if e.Kz83 != 178 {
		t.Errorf("due: %v", e.Kz83)
	}

	filed := map[string]float64{"81": 1000, "86": 100, "21": 500, "46": 100, "47": 19, "84": 50, "85": 9.5, "66": 19, "67": 28.5, "83": 178}
	if c := e.Changed(filed); len(c) != 0 {
		t.Errorf("unchanged: %+v", c)
	}
	filed["81"] = 900
	if c := e.Changed(filed); len(c) != 1 || c[0].Kz != "81" {
		t.Errorf("changed: %+v", c)
	}
}
