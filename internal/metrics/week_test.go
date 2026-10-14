package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// An appointment naming a customer and a visit there leave gaps unless
// Kimai has that customer on the day; both on one day make one gap with
// the appointment's times. The proposal is the customer's latest project.
func TestWeekGaps(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Fotohaus Elbe"}, {ID: 2, Name: "Werft"}},
		Projects:  []sources.KimaiProject{{ID: 10, Name: "Kampagne Herbst", CustomerID: 1}},
		Timesheets: []sources.KimaiSheet{
			{Begin: "2026-09-01T09:00:00", CustomerID: 1, ProjectID: 9, Activity: "Schnitt"},
			{Begin: "2026-09-02T09:00:00", CustomerID: 1, ProjectID: 10, Activity: "Dreh"},
			{Begin: "2026-09-09T09:00:00", CustomerID: 2, ProjectID: 20, Activity: "Ton"},
		},
	}
	at := func(s string) time.Time { v, _ := time.ParseInLocation("2006-01-02 15:04", s, loc); return v }
	cal := &sources.CalendarResult{Events: []sources.Event{
		{Start: at("2026-09-08 10:00"), End: at("2026-09-08 12:30"), Title: "Dreh Kampagne Herbst"},
		{Start: at("2026-09-09 10:00"), End: at("2026-09-09 11:00"), Title: "Werft Abnahme"}, // booked
		{Start: at("2026-09-10 10:00"), End: at("2026-09-10 11:00"), Title: "Zahnarzt"},
	}}
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	gaps := metrics.WeekGaps(kimai, nil, nil, cal, start, loc)
	if len(gaps) != 1 || gaps[0].CustomerID != 1 || gaps[0].Minutes() != 150 || gaps[0].Start.Hour() != 10 {
		t.Fatalf("gaps: %+v", gaps)
	}
	if p := metrics.WeekProposals(kimai)[1]; p.ProjectID != 10 || p.Activity != "Dreh" {
		t.Errorf("proposal: %+v", p)
	}
}
