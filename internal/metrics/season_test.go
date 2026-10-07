package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

func TestNinjaSeasonal(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	data := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Status: "paid", Date: "2024-09-05", Net: 100},
		{Status: "paid", Date: "2025-09-05", Net: 300},
		{Status: "paid", Date: "2026-09-05", Net: 500},
	}}

	// 2023 has no invoices yet, so September averages 2024 and 2025, and
	// says where the data begins.
	months, from := NinjaSeasonal(data, today, 1)
	if len(months) != 1 || months[0].Net != 500 || months[0].Prev != 200 {
		t.Fatalf("unexpected: %+v", months)
	}
	if !from.Equal(time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("data from %s", from)
	}
}

// Every month averages the same years, and only years with data for the
// whole month: history from August 2024 leaves July 2024 out, so all
// three months average 2025 alone (not September over 2024 and 2025,
// and never a July 2024 counted as 0).
func TestNinjaSeasonalFullYears(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	data := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{
		{Status: "paid", Date: "2024-08-05", Net: 900},
		{Status: "paid", Date: "2024-09-05", Net: 100},
		{Status: "paid", Date: "2025-07-05", Net: 70},
		{Status: "paid", Date: "2025-08-05", Net: 80},
		{Status: "paid", Date: "2025-09-05", Net: 300},
	}}
	months, from := NinjaSeasonal(data, today, 3)
	want := []float64{70, 80, 300}
	for i, m := range months {
		if m.Prev != want[i] {
			t.Fatalf("%s: Ø %v, want %v", m.Month, m.Prev, want[i])
		}
	}
	if !from.Equal(time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("data from %s", from)
	}

	// Three full years: no "data from".
	data.Invoices = append(data.Invoices, sources.NinjaInvoice{Status: "paid", Date: "2023-01-05", Net: 1})
	if _, from := NinjaSeasonal(data, today, 3); !from.IsZero() {
		t.Fatalf("full history marked from %s", from)
	}
}
