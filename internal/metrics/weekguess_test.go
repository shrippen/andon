package metrics_test

import (
	"testing"

	"andon/internal/bayes"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// Kimai's entries teach project and activity by their words; a gap gets
// the pair its title names, only of its own customer, never without a
// known word. A booked gap teaches its place.
func TestWeekGuess(t *testing.T) {
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Fotohaus Elbe"}, {ID: 2, Name: "Werft"}},
		Projects:  []sources.KimaiProject{{ID: 10, Name: "Kampagne Herbst", CustomerID: 1}, {ID: 11, Name: "Imagefilm", CustomerID: 1}, {ID: 20, Name: "Hafen", CustomerID: 2}},
		Timesheets: []sources.KimaiSheet{
			{ID: 1, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh am Elbufer"},
			{ID: 2, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh Innenräume"},
			{ID: 3, CustomerID: 1, ProjectID: 11, Activity: "Schnitt", Notes: "Rohschnitt"},
			{ID: 4, CustomerID: 1, ProjectID: 11, Activity: "Schnitt", Notes: "Feinschnitt Farbkorrektur"},
			{ID: 5, CustomerID: 2, ProjectID: 20, Activity: "Ton", Notes: "Tonaufnahme"},
			{ID: 6, CustomerID: 2, ProjectID: 0, Activity: "Ton"}, // no project: not learned
		},
	}
	m := bayes.New()
	if !metrics.WeekLearn(m, kimai) || m.Stats().Examples != 5 || metrics.WeekLearn(m, kimai) {
		t.Fatalf("learn: %+v", m.Stats())
	}
	g := metrics.WeekGuesserOf(kimai, bayes.Use{Model: bayes.New(), MinP: 0.6})

	cases := []struct {
		gap  metrics.WeekGap
		want metrics.WeekProposal
	}{
		{metrics.WeekGap{CustomerID: 1, Title: "Fotohaus Elbe: Farbkorrektur Imagefilm"}, metrics.WeekProposal{ProjectID: 11, Activity: "Schnitt"}},
		{metrics.WeekGap{CustomerID: 1, Title: "Fotohaus Elbe Dreh"}, metrics.WeekProposal{ProjectID: 10, Activity: "Dreh"}},
		{metrics.WeekGap{CustomerID: 1, Title: "Zahnarzt"}, metrics.WeekProposal{}},        // no known word
		{metrics.WeekGap{CustomerID: 3, Title: "Farbkorrektur"}, metrics.WeekProposal{}},   // another customer's project
		{metrics.WeekGap{CustomerID: 1, Title: "Studio Neustadt"}, metrics.WeekProposal{}}, // a place not booked yet
	}
	for i, c := range cases {
		if got, _ := g.Guess(c.gap); got != c.want {
			t.Errorf("%d %q: %+v, want %+v", i, c.gap.Title, got, c.want)
		}
	}

	// The user books the place once: it is learned.
	place := metrics.WeekGap{Key: "visit:2026-09-08:1", CustomerID: 1, Title: "Studio Neustadt"}
	use := bayes.Use{Model: bayes.New(), MinP: 0.6}
	if !metrics.WeekLearnGap(use.Model, place, metrics.WeekProposal{ProjectID: 10, Activity: "Dreh"}) {
		t.Fatal("gap not learned")
	}
	g = metrics.WeekGuesserOf(kimai, use)
	if got, ok := g.Guess(place); !ok || got.ProjectID != 10 || got.Activity != "Dreh" {
		t.Errorf("place: %+v %v", got, ok)
	}
}
