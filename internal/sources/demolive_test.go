package sources

import (
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
