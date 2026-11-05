package metrics_test

import (
	"testing"
	"time"

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

// elsterSpan is a return's month.
func elsterSpan(t *testing.T, first, last string) (time.Time, time.Time) {
	t.Helper()
	start, ok := metrics.ParseDay(first)
	end, ok2 := metrics.ParseDay(last)
	if !ok || !ok2 {
		t.Fatal("bad day")
	}
	return start, end
}

// checkOf is the first check of kind, for a look at its fields.
func checkOf(e metrics.Elster, kind metrics.CheckKind) *metrics.ElsterCheck {
	for i := range e.Checks {
		if e.Checks[i].Kind == kind {
			return &e.Checks[i]
		}
	}
	return nil
}

// § 18b UStG: services to EU businesses go in the month they were
// performed, also for a return by payments, as in the ZM.
func TestElsterKz21ByInvoiceDay(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Clients: []sources.NinjaClient{{ID: 2, CountryID: "208", VATNumber: "DK1"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 2, Status: "paid", Date: "2026-08-15", Net: 1000, Amount: 1000}},
		Payments: []sources.NinjaPayment{{ID: 1, ClientID: 2, Date: "2026-09-05", Amount: 1000}}}

	augStart, augEnd := elsterSpan(t, "2026-08-01", "2026-08-31")
	sepStart, sepEnd := elsterSpan(t, "2026-09-01", "2026-09-30")
	if got := metrics.ElsterOf(n, augStart, augEnd, "ist").Kz21; got != 1000 {
		t.Errorf("August, invoiced: Kz 21 = %v, want 1000", got)
	}
	if got := metrics.ElsterOf(n, sepStart, sepEnd, "ist").Kz21; got != 0 {
		t.Errorf("September, paid: Kz 21 = %v, want 0", got)
	}
}

// A rate of no line is not dropped silently: the page asks.
func TestElsterOddRateIsChecked(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Clients: []sources.NinjaClient{{ID: 1, Name: "Acme", CountryID: "276"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, Number: "R1", ClientID: 1, Status: "sent", Date: "2026-09-10", Net: 1000, Taxes: 130, Amount: 1130}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	c := checkOf(metrics.ElsterOf(n, start, end, "soll"), metrics.CheckRate)
	if c == nil || c.Number != "R1" || c.Name != "Acme" || c.Amount != 1000 {
		t.Errorf("check: %+v", c)
	}
}

// By payments, an odd invoice paid in two parts is one check, of what
// was paid.
func TestElsterOddRatePaidOnce(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Clients: []sources.NinjaClient{{ID: 1, CountryID: "276"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, Number: "R1", ClientID: 1, Status: "paid", Date: "2026-09-10", Net: 1000, Taxes: 130, Amount: 1130}},
		Payments: []sources.NinjaPayment{{ID: 1, ClientID: 1, Date: "2026-09-11", Amount: 565}, {ID: 2, ClientID: 1, Date: "2026-09-12", Amount: 282.5}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "ist")
	if len(e.Checks) != 1 || e.Checks[0].Amount != 750 {
		t.Errorf("checks: %+v", e.Checks)
	}
}

// 0 % at home is no Kz 45: only a client known to be abroad is.
func TestElsterZeroAtHome(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276",
		Clients: []sources.NinjaClient{{ID: 1, CountryID: "276"}, {ID: 2}, {ID: 3, CountryID: "840"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 1, Status: "sent", Date: "2026-09-10", Net: 1000, Amount: 1000},
			{ID: 2, ClientID: 2, Status: "sent", Date: "2026-09-10", Net: 200, Amount: 200},
			{ID: 3, ClientID: 3, Status: "sent", Date: "2026-09-10", Net: 30, Amount: 30}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "soll")
	if e.Kz45 != 30 {
		t.Errorf("Kz 45 = %v, want 30", e.Kz45)
	}
	if len(e.Checks) != 2 || e.Checks[0].Kind != metrics.CheckZero || e.Checks[1].Kind != metrics.CheckZero {
		t.Errorf("checks: %+v", e.Checks)
	}
}

// A foreign rate is foreign VAT: no input VAT here. German VAT billed
// from abroad (a vendor registered here) stays.
func TestElsterForeignTax(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Vendors: []sources.NinjaVendor{{Key: "ie", Name: "Cloud IE", CountryID: "372"}},
		Expenses: []sources.NinjaExpense{{Number: "A1", VendorKey: "ie", Date: "2026-09-10", Amount: 123, Tax: 23, TaxRate: 23},
			{Number: "A2", VendorKey: "ie", Date: "2026-09-11", Amount: 119, Tax: 19, TaxRate: 19}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "soll")
	if e.Kz66 != 19 {
		t.Errorf("Kz 66 = %v, want 19", e.Kz66)
	}
	if c := checkOf(e, metrics.CheckForeignTax); c == nil || c.Number != "A1" || c.Name != "Cloud IE" {
		t.Errorf("check: %+v", c)
	}
	if got := metrics.NinjaInputVAT(n, start, end); got != 19 {
		t.Errorf("input VAT = %v, want 19", got)
	}
}

// No tax and no vendor country: whether § 13b applies is unknown.
func TestElsterNoCountry(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Vendors: []sources.NinjaVendor{{Key: "x", Name: "ZSA"}},
		Expenses: []sources.NinjaExpense{{Number: "A1", VendorKey: "x", Date: "2026-09-10", Amount: 100}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	c := checkOf(metrics.ElsterOf(n, start, end, "soll"), metrics.CheckNoCountry)
	if c == nil || c.Name != "ZSA" || c.Amount != 100 {
		t.Errorf("check: %+v", c)
	}
}

// The bank's amount booked with tax on top: Invoice Ninja, and so Kz 66,
// counts more input VAT than was paid. A share of a booking (a 50/50
// ticket) is net or gross on purpose and not flagged.
func TestElsterTaxOnTop(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", BankRead: true,
		Vendors:  []sources.NinjaVendor{{Key: "a", Name: "Cloud", CountryID: "276"}},
		BankTxns: []sources.NinjaBankTxn{{Key: "t1", Amount: 119}, {Key: "t2", Amount: 63}},
		Expenses: []sources.NinjaExpense{{Number: "A1", VendorKey: "a", Date: "2026-09-10", Amount: 119, Tax: 22.61, TaxRate: 19, TxnKey: "t1"},
			{Number: "A2", VendorKey: "a", Date: "2026-09-11", Amount: 31.5, Tax: 2.2, TaxRate: 7, TxnKey: "t2"}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "soll")
	if len(e.Checks) != 1 {
		t.Fatalf("checks: %+v", e.Checks)
	}
	if c := e.Checks[0]; c.Kind != metrics.CheckTaxOnTop || c.Number != "A1" || c.Diff != 3.61 {
		t.Errorf("check: %+v", c)
	}
}

// A refund leaves the paid month's figures: it reduces the month it
// happened in, which Invoice Ninja does not say, so the page asks.
func TestElsterRefundIsChecked(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Clients: []sources.NinjaClient{{ID: 1, Name: "Acme", CountryID: "276"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 1, Status: "paid", Date: "2026-09-01", Net: 100, Taxes: 19, Amount: 119}},
		Payments: []sources.NinjaPayment{{ID: 1, Number: "P1", ClientID: 1, Date: "2026-09-05", Amount: 119, Refunded: 119}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "ist")
	if e.Kz81 != 100 {
		t.Errorf("Kz 81 = %v, want 100", e.Kz81)
	}
	if c := checkOf(e, metrics.CheckRefund); c == nil || c.Number != "P1" || c.Name != "Acme" || c.Amount != 119 {
		t.Errorf("check: %+v", c)
	}
}

// A foreign vendor's tax without a rate is no input VAT either.
func TestElsterForeignTaxNoRate(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Vendors: []sources.NinjaVendor{{Key: "ie", Name: "Cloud IE", CountryID: "372"}},
		Expenses: []sources.NinjaExpense{{Number: "A1", VendorKey: "ie", Date: "2026-09-10", Amount: 123, Tax: 23}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	e := metrics.ElsterOf(n, start, end, "soll")
	if e.Kz66 != 0 || checkOf(e, metrics.CheckForeignTax) == nil {
		t.Errorf("Kz 66 = %v, checks %+v", e.Kz66, e.Checks)
	}
}

// No vendor at all (a bank fee) and a 0 € invoice ask nothing.
func TestElsterNoNoise(t *testing.T) {
	n := &sources.NinjaDataset{HomeCountryID: "276", Clients: []sources.NinjaClient{{ID: 1, CountryID: "276"}},
		Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 1, Status: "sent", Date: "2026-09-10"}},
		Expenses: []sources.NinjaExpense{{Number: "A1", Date: "2026-09-10", Amount: 5}}}
	start, end := elsterSpan(t, "2026-09-01", "2026-09-30")
	if e := metrics.ElsterOf(n, start, end, "soll"); len(e.Checks) != 0 {
		t.Errorf("checks: %+v", e.Checks)
	}
}
