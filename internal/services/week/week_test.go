package week

import (
	"context"
	"slices"
	"testing"
	"time"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/learn"
	"andon/internal/services/workbench"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// A gap books with project, activity and sane times; a booked or unknown
// gap, a missing activity and reversed times are refused.
func TestCheck(t *testing.T) {
	start := time.Date(2026, 9, 8, 10, 0, 0, 0, time.Local)
	b := book{
		kimai:   &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 1, Name: "Fotohaus Elbe"}}},
		catalog: &sources.KimaiCatalog{Projects: []sources.KimaiPick{{ID: 10, Name: "Kampagne"}}, Activities: []sources.KimaiActivityPick{{ID: 5, Name: "Dreh"}}},
		gaps:    []metrics.WeekGap{{Key: "event:x", CustomerID: 1, Start: start, End: start.Add(2 * time.Hour)}},
	}
	fields := func(project, activity, begin, end string) map[string]string {
		return map[string]string{FieldProject: project, FieldActivity: activity, FieldBegin: begin, FieldEnd: end, FieldNote: "Dreh"}
	}
	cases := []struct {
		step workbench.Step
		want workbench.Problem
	}{
		{workbench.Step{Case: "event:x", Action: ActBook, Fields: fields("10", "5", "2026-09-08T10:00", "2026-09-08T12:00")}, workbench.ProblemNone},
		{workbench.Step{Case: "event:y", Action: ActBook, Fields: fields("10", "5", "2026-09-08T10:00", "2026-09-08T12:00")}, ProblemGone},
		{workbench.Step{Case: "event:x", Action: ActBook, Fields: fields("10", "", "2026-09-08T10:00", "2026-09-08T12:00")}, ProblemTarget},
		{workbench.Step{Case: "event:x", Action: ActBook, Fields: fields("10", "5", "2026-09-08T12:00", "2026-09-08T10:00")}, ProblemTimes},
	}
	for i, c := range cases {
		it, problem := b.Check(&c.step)
		if problem != c.want {
			t.Errorf("%d: %q, want %q", i, problem, c.want)
		}
		if i == 0 && (it.Project != "Kampagne" || it.Activity != "Dreh" || it.Customer != "Fotohaus Elbe") {
			t.Errorf("item: %+v", it)
		}
	}
}

// The page opens on the demo Kimai, last week by default.
func TestOpen(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, "demo://kimai")
	today := time.Now()
	p, err := Open(context.Background(), d, who, "", time.Time{}, today)
	if err != nil || len(p.Scopes) != 1 || !p.Week.Equal(metrics.WeekStart(metrics.Today(today)).AddDate(0, 0, -7)) || len(p.Weeks) != weeksShown {
		t.Fatalf("open: %+v %v", p, err)
	}
}

// The customer's latest entry fills the selects; what the model learned
// from Kimai's entries is only suggested: here the activity the title
// names, while the latest entry's activity is gone.
func TestCasesLearned(t *testing.T) {
	sheet := func(id, customer, project int64, day, activity, notes string) sources.KimaiSheet {
		return sources.KimaiSheet{ID: id, Begin: day + "T09:00:00", CustomerID: customer, ProjectID: project, Activity: activity, Notes: notes}
	}
	b := book{
		kimai: &sources.KimaiDataset{
			Customers: []sources.KimaiCustomer{{ID: 1, Name: "Fotohaus Elbe"}, {ID: 2, Name: "Werft"}},
			Projects:  []sources.KimaiProject{{ID: 10, Name: "Kampagne", CustomerID: 1}, {ID: 20, Name: "Hafen", CustomerID: 2}},
			Timesheets: []sources.KimaiSheet{
				sheet(1, 1, 10, "2026-09-01", "Dreh", "Dreh am Elbufer"),
				sheet(2, 1, 10, "2026-09-02", "Dreh", "Dreh innen"),
				sheet(3, 1, 10, "2026-09-03", "Archiv", ""), // latest, its activity gone
				sheet(4, 2, 20, "2026-09-03", "Ton", "Tonaufnahme"),
			},
		},
		catalog: &sources.KimaiCatalog{Projects: []sources.KimaiPick{{ID: 10, Name: "Kampagne"}, {ID: 20, Name: "Hafen"}},
			Activities: []sources.KimaiActivityPick{{ID: 5, Name: "Dreh"}, {ID: 6, Name: "Ton"}}},
		gaps: []metrics.WeekGap{{Key: "event:a", CustomerID: 1, Title: "Fotohaus Elbe Dreh"}, {Key: "event:b", CustomerID: 2, Title: "Werft"}},
	}

	got := b.cases(bayes.Use{Model: bayes.New(), MinP: 0.6})
	c := got[0]
	if c.Project != 10 || c.Activity != 0 || c.Checked {
		t.Errorf("learned filled in: %+v", c)
	}
	// Project 10 is in its select already: only the activity is suggested.
	if want := []metrics.Pick{{Value: "5", Label: "Dreh"}}; len(c.ProjectPicks) != 0 || !slices.Equal(c.ActivityPicks, want) {
		t.Errorf("picks: %+v %+v", c.ProjectPicks, c.ActivityPicks)
	}
	if c := got[1]; c.Project != 20 || c.Activity != 6 || !c.Checked {
		t.Errorf("latest: %+v", c)
	}

	// Learning off: the latest entry's project, nothing suggested.
	if c := b.cases(bayes.Use{})[0]; c.Project != 10 || c.Activity != 0 || c.Checked || c.ActivityPicks != nil {
		t.Errorf("off: %+v", c)
	}
}

// A booked gap is stored in the Kimai's model; a failed one is not.
func TestTeachBooked(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn, err := content.Connection(d, testkit.Conn(t, d, who, space, enums.ServiceKimai, "demo://kimai"))
	if err != nil {
		t.Fatal(err)
	}
	step := func(key string) workbench.Planned[Item] {
		return workbench.Planned[Item]{Step: workbench.Step{Case: key, Action: ActBook},
			Item: Item{Gap: metrics.WeekGap{Key: key, CustomerID: 1, Title: "Studio Neustadt"}, ProjectID: 10, Activity: "Dreh"}}
	}
	plan := workbench.Plan[Item]{Steps: []workbench.Planned[Item]{step("visit:a"), step("visit:b")}}
	out := workbench.Result{Done: 1, Failed: []workbench.Failure{{Case: "visit:b"}}}

	book{conn: conn}.teachBooked(d, who, plan, out)
	if l := learn.Model(d, purposeTask, conn, model.UserHolder(who.UserID)); l.Model == nil || l.Model.Stats().Examples != 1 {
		t.Fatalf("model: %+v", l)
	}
}

// The purpose has its text.
func TestTexts(t *testing.T) {
	for _, k := range []string{"learn.purpose_" + purposeTask} {
		if !i18n.Has(k) {
			t.Errorf("no text %s", k)
		}
	}
}
