package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// A Monday with appointments at 10 and 15 leaves 11–15 and 16–18 free;
// the hour before 10 is too short. The weekend is skipped.
func TestFreeGaps(t *testing.T) {
	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	evs := []calEvent{{e: sources.Event{Start: monday.Add(10 * time.Hour)}}, {e: sources.Event{Start: monday.Add(15 * time.Hour)}}}
	rows := freeGaps(evs, monday, time.UTC)

	var got []string
	for _, r := range rows {
		if r[0].Value.(map[string]any)["$day"] == "2026-09-28" {
			got = append(got, r[1].Value.(string)+"-"+r[2].Value.(string))
		}
	}
	want := []string{"11:00-15:00", "16:00-18:00"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}

	// 10 workdays in 14 days, 9 of them free all day
	if len(rows) != len(want)+9 {
		t.Fatalf("rows %d", len(rows))
	}
}
