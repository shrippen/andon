package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// Linear by month from the purchase month: 1 200 € over 12 months,
// bought in September, is 400 € in its first year and 800 € in the
// next; an asset without rule keeps its cost; a later one is left out,
// as are assets done depreciating.
func TestDepreciation(t *testing.T) {
	snipe := &sources.SnipeDataset{Assets: []sources.SnipeAsset{
		{Name: "Laptop", PurchaseDate: "2025-09-14", PurchaseCost: 1200, DepMonths: 12},
		{Name: "Licht", PurchaseDate: "2025-02-18", PurchaseCost: 37.87},
		{Name: "Neu", PurchaseDate: "2027-01-02", PurchaseCost: 100, DepMonths: 12},
	}}
	got := Depreciation(snipe, 2025)
	if len(got) != 2 || got[0].Asset.Name != "Licht" || got[0].Year != 0 || got[0].Left != 37.87 || !got[0].Instant {
		t.Fatalf("2025: %+v", got)
	}
	if l := got[1]; l.Year != 400 || l.Left != 800 || l.Instant {
		t.Errorf("laptop 2025: %+v", l)
	}
	if got := Depreciation(snipe, 2026); len(got) != 1 || got[0].Year != 800 || got[0].Left != 0 {
		t.Errorf("2026: only the laptop, got %+v", got)
	}
	if got := Depreciation(snipe, 2027); len(got) != 1 || got[0].Asset.Name != "Neu" {
		t.Errorf("2027: only the new asset, got %+v", got)
	}
}

// Days with work in Kimai and no business ride count as home office;
// the allowance stops at the maximum days.
func TestHomeOffice(t *testing.T) {
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-03-02T09:00:00", Minutes: 240},
		{Begin: "2026-03-02T14:00:00", Minutes: 120},
		{Begin: "2026-03-03T09:00:00", Minutes: 300},
		{Begin: "2025-12-30T09:00:00", Minutes: 60},
	}}
	away := time.Date(2026, 3, 3, 10, 0, 0, 0, time.Local)
	travel := &Travel{Rides: []ClassedRide{{Ride: Ride{Start: away, End: away.Add(time.Hour)}, Class: ClassBusiness}}}

	days, estimated := HomeOffice(kimai, travel, 2026)
	if estimated || len(days) != 1 || days[0].Minutes != 360 || days[0].Day.Day() != 2 {
		t.Fatalf("days: %+v %v", days, estimated)
	}
	if days, estimated = HomeOffice(kimai, nil, 2026); !estimated || len(days) != 2 {
		t.Errorf("without travel: %+v %v", days, estimated)
	}
	if got := HomeOfficeAllowance(230, HomeOfficeRate, HomeOfficeMaxDays); got != 1260 {
		t.Errorf("allowance = %v, want 1260", got)
	}
}

// Expenses add up by their EÜR line; those without one form their own.
func TestEuerLines(t *testing.T) {
	ninja := &sources.NinjaDataset{Expenses: []sources.NinjaExpense{
		{Date: "2026-01-10", Amount: 119, Tax: 19, Custom: [sources.NinjaSlots]string{"", "", "Laufende EDV-Kosten"}},
		{Date: "2026-02-10", Amount: 59.5, Tax: 9.5, Custom: [sources.NinjaSlots]string{"", "", "Laufende EDV-Kosten"}},
		{Date: "2026-03-10", Amount: 20},
		{Date: "2025-03-10", Amount: 999},
	}}
	got := EuerLines(ninja, 3, 2026)
	if len(got) != 2 || got[0].Line != "Laufende EDV-Kosten" || got[0].Net != 150 || got[0].Tax != 28.5 || got[0].Expenses != 2 || got[1].Line != "" || got[1].Net != 20 {
		t.Errorf("lines: %+v", got)
	}
}

// The review counts the year, compares with the one before only when
// the data reaches back, names the top clients and the new ones.
func TestReviewYear(t *testing.T) {
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-03-02T09:00:00", Minutes: 600}, {Begin: "2026-05-02T09:00:00", Minutes: 120}, {Begin: "2025-06-02T09:00:00", Minutes: 60},
	}}
	ninja := &sources.NinjaDataset{Since: "2025-01-01", Clients: []sources.NinjaClient{{ID: 1, Name: "Werft"}, {ID: 2, Name: "Stiftung"}},
		Invoices: []sources.NinjaInvoice{
			{ClientID: 1, Status: "paid", Date: "2025-04-01", Net: 500},
			{ClientID: 1, Status: "paid", Date: "2026-04-01", Net: 1000},
			{ClientID: 2, Status: "sent", Date: "2026-05-01", Net: 2000},
			{ClientID: 2, Status: "draft", Date: "2026-06-01", Net: 9999},
		}}
	r := ReviewYear(kimai, ninja, 2026)
	if r.Hours != 12 || r.Revenue != 3000 || r.PriorRevenue != 500 || !r.PriorRevenueKnown || r.PriorHoursKnown || r.Busiest.Month() != time.March {
		t.Fatalf("review: %+v", r)
	}
	if len(r.Top) != 2 || r.Top[0].Name != "Stiftung" || len(r.New) != 1 || r.New[0] != "Stiftung" {
		t.Errorf("clients: %+v %v", r.Top, r.New)
	}
}
