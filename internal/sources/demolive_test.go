package sources

import (
	"strings"
	"testing"
	"time"
)

// The demo's "today" in Kimai Lite is its blocks plus the running timer,
// not a number of its own (it read 5:12 beside blocks of 4:12).
func TestDemoKimaiLiveTodayAddsUp(t *testing.T) {
	now := time.Date(2026, 10, 7, 16, 0, 0, 0, time.Local)
	live := DemoKimaiLive(now)
	sum := 0
	for _, s := range live.Today {
		sum += int(s.End.Sub(s.Begin).Minutes())
	}
	for _, a := range live.Active {
		sum += int(now.Sub(a.Begin).Minutes())
	}
	if live.TodayMin != sum {
		t.Fatalf("today %d min, blocks and timer %d min", live.TodayMin, sum)
	}
	if live.WeekMin < live.TodayMin {
		t.Fatalf("week %d below today %d", live.WeekMin, live.TodayMin)
	}
}

// The demo tells one timer story: the Kimai dataset (Kimai tile, Today,
// rules) runs the same timer as Kimai Lite, and books today's blocks
// too, so "today" adds up to the same everywhere.
func TestDemoKimaiOneTimer(t *testing.T) {
	now := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	data, live := DemoKimai(now), DemoKimaiLive(now)
	if len(data.Active) != 1 || len(live.Active) != 1 {
		t.Fatalf("running: dataset %d, live %d", len(data.Active), len(live.Active))
	}
	begin, err := time.Parse(time.RFC3339, data.Active[0].Begin)
	if err != nil || !begin.Equal(live.Active[0].Begin) || data.Active[0].ID != live.Active[0].ID {
		t.Fatalf("dataset timer %d from %s, live %d from %s", data.Active[0].ID, data.Active[0].Begin, live.Active[0].ID, live.Active[0].Begin)
	}
	if data.Active[0].ProjectID != live.Active[0].ProjectID {
		t.Fatalf("project: dataset %d, live %d", data.Active[0].ProjectID, live.Active[0].ProjectID)
	}

	today := demoDay(now).Format(time.DateOnly)
	sum := int(now.Sub(begin).Minutes())
	for _, s := range data.Timesheets {
		if strings.HasPrefix(s.Begin, today) {
			sum += s.Minutes
		}
	}
	if sum != live.TodayMin {
		t.Fatalf("today: dataset %d min, Kimai Lite %d min", sum, live.TodayMin)
	}
}
