package metrics_test

import (
	"testing"

	"andon/internal/bayes"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// Kimai's entries teach project and activity by their words; a gap's
// first suggestion is the pair its title names, only of its own
// customer, never without a known word beside its customer's and
// projects' names. A booked gap teaches its place.
func TestWeekGuess(t *testing.T) {
	first := func(ps []metrics.WeekProposal) metrics.WeekProposal {
		if len(ps) == 0 {
			return metrics.WeekProposal{}
		}
		return ps[0]
	}
	kimai := &sources.KimaiDataset{
		Customers: []sources.KimaiCustomer{{ID: 1, Name: "Fotohaus Elbe"}, {ID: 2, Name: "Werft"}},
		Projects:  []sources.KimaiProject{{ID: 10, Name: "Kampagne Herbst", CustomerID: 1}, {ID: 11, Name: "Imagefilm", CustomerID: 1}, {ID: 20, Name: "Hafen", CustomerID: 2}},
		Timesheets: []sources.KimaiSheet{
			{ID: 1, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh am Elbufer"},
			{ID: 2, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh Innenräume"},
			{ID: 7, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh Außen"},
			{ID: 8, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh Werkhalle"},
			{ID: 3, CustomerID: 1, ProjectID: 11, Activity: "Schnitt", Notes: "Rohschnitt"},
			{ID: 4, CustomerID: 1, ProjectID: 11, Activity: "Schnitt", Notes: "Feinschnitt Farbkorrektur"},
			{ID: 5, CustomerID: 2, ProjectID: 20, Activity: "Ton", Notes: "Tonaufnahme"},
			{ID: 9, CustomerID: 2, ProjectID: 20, Activity: "Ton", Notes: "Telefonat"},
			{ID: 6, CustomerID: 2, ProjectID: 0, Activity: "Ton"}, // no project: not learned
		},
	}
	m := bayes.New()
	if !metrics.WeekLearn(m, kimai) || m.Stats().Examples != 8 || metrics.WeekLearn(m, kimai) {
		t.Fatalf("learn: %+v", m.Stats())
	}
	g := metrics.WeekGuesserOf(kimai, bayes.Use{Model: bayes.New(), MinP: 0.6})

	cases := []struct {
		gap  metrics.WeekGap
		want metrics.WeekProposal
	}{
		{metrics.WeekGap{CustomerID: 1, Title: "Fotohaus Elbe: Farbkorrektur Imagefilm"}, metrics.WeekProposal{ProjectID: 11, Activity: "Schnitt"}},
		{metrics.WeekGap{CustomerID: 1, Title: "Fotohaus Elbe Dreh"}, metrics.WeekProposal{ProjectID: 10, Activity: "Dreh"}},
		{metrics.WeekGap{CustomerID: 1, Title: "Zahnarzt"}, metrics.WeekProposal{}},                         // no known word
		{metrics.WeekGap{CustomerID: 3, Title: "Farbkorrektur"}, metrics.WeekProposal{}},                    // another customer's project
		{metrics.WeekGap{CustomerID: 1, Title: "Studio Neustadt"}, metrics.WeekProposal{}},                  // a place not booked yet
		{metrics.WeekGap{CustomerID: 1, Title: "Telefonat Fotohaus Elbe"}, metrics.WeekProposal{}},          // "Telefonat" known only from customer 2
		{metrics.WeekGap{CustomerID: 1, Title: "Telefonat Fotohaus Elbe Kampagne"}, metrics.WeekProposal{}}, // and a project name
		{metrics.WeekGap{CustomerID: 1, Title: "Fotohaus Elbe Kampagne Herbst"}, metrics.WeekProposal{}},    // only a project's name known
	}
	for i, c := range cases {
		if got := first(g.Suggest(c.gap, 3)); got != c.want {
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
	if got := g.Suggest(place, 3); len(got) == 0 || got[0].ProjectID != 10 || got[0].Activity != "Dreh" {
		t.Errorf("place: %+v", got)
	}
}

// An entry gone from Kimai's data (deleted, or older than the data's
// window) is forgotten; a booked gap stays; data without entries
// forgets nothing.
func TestWeekLearnForgets(t *testing.T) {
	sheet := func(id int64) sources.KimaiSheet {
		return sources.KimaiSheet{ID: id, CustomerID: 1, ProjectID: 10, Activity: "Dreh", Notes: "Dreh"}
	}
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{sheet(1), sheet(2)}}
	m := bayes.New()
	metrics.WeekLearn(m, kimai)
	metrics.WeekLearnGap(m, metrics.WeekGap{Key: "visit:a", CustomerID: 1, Title: "Studio"}, metrics.WeekProposal{ProjectID: 10, Activity: "Dreh"})

	if metrics.WeekLearn(m, &sources.KimaiDataset{}) || m.Stats().Examples != 3 {
		t.Fatalf("empty data forgot: %+v", m.Stats())
	}
	kimai.Timesheets = kimai.Timesheets[1:]
	if !metrics.WeekLearn(m, kimai) || m.Stats().Examples != 2 {
		t.Fatalf("after sheet 1 went: %+v", m.Stats())
	}
}
