package week

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
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
