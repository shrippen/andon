package metrics

import (
	"testing"

	"andon/internal/sources"
)

// A draft line bills Kimai's hourly rate for the exact hours: three
// 20-minute sheets at 50 €/h are 1 h at 50 €, not 50.01 € from summed
// per-sheet amounts. Without an hourly rate it follows from the exact
// hours: 7 minutes for 5.83 € are 0.1167 h at 50 €, not 0.12 h at 48.58 €.
func TestDraftRates(t *testing.T) {
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{ID: 1, Begin: "2026-09-01", End: "x", Minutes: 20, Rate: 16.67, HourlyRate: 50, Billable: true, CustomerID: 1, Activity: "A"},
		{ID: 2, Begin: "2026-09-02", End: "x", Minutes: 20, Rate: 16.67, HourlyRate: 50, Billable: true, CustomerID: 1, Activity: "A"},
		{ID: 3, Begin: "2026-09-03", End: "x", Minutes: 20, Rate: 16.67, HourlyRate: 50, Billable: true, CustomerID: 1, Activity: "A"},
		{ID: 4, Begin: "2026-09-03", End: "x", Minutes: 7, Rate: 5.8333, Billable: true, CustomerID: 2, Activity: "B"},
	}}
	rates := map[int64]DraftLine{}
	for _, d := range Drafts(kimai, nil, nil) {
		rates[d.CustomerID] = d.Lines[0]
	}
	if l := rates[1]; l.Rate != 50 || l.Hours != 1 {
		t.Fatalf("hourly rate: %+v", l)
	}
	if l := rates[2]; l.Rate != 50 || l.Hours >= 0.12 {
		t.Fatalf("derived rate: %+v", l)
	}
}
