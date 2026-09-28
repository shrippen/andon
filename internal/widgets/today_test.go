package widgets_test

import (
	"testing"
	"time"

	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestTodayTimeline: today's appointments, the running timer and the
// next departures in time order, deadlines of the week after them;
// other days' appointments stay out.
func TestTodayTimeline(t *testing.T) {
	kind, ok := widgets.Get("today")
	if !ok {
		t.Fatal("today not registered")
	}
	cfg, _ := widgets.Decode("today", map[string]any{"timezone": "UTC", "stop": "Hbf", "days": 40.0})
	now := time.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	results := map[string]any{
		"calendar": &sources.CalendarResult{Events: []sources.Event{
			{Start: day.Add(23*time.Hour + 30*time.Minute), Title: "Spät"},
			{Start: day, Title: "Früh"}, // midnight: before the timer at any time of day
			{Start: day.AddDate(0, 0, 1).Add(9 * time.Hour), Title: "Morgen"},
		}},
		"kimai": &sources.KimaiDataset{Active: []sources.KimaiSheet{{Begin: now.Format(time.RFC3339), Activity: "Relaunch"}}},
		"board": &sources.BoardResult{Movements: []sources.Movement{{When: now.Add(20 * time.Minute), Line: "S1", Place: "Flughafen"}}},
	}
	settings := map[string]any{"tax": map[string]any{"vat": map[string]any{"return_interval": "monthly"}}}
	view := kind.View(cfg, results, widgets.ViewCtx{Today: now.Format(time.DateOnly), Settings: settings})
	items := view["Items"].([]widgets.TodayItem)
	var kinds, titles []string
	for _, it := range items {
		kinds = append(kinds, it.Kind)
		titles = append(titles, it.Text)
	}
	if len(items) < 5 || items[0].Text != "Früh" || items[0].Kind != "event" {
		t.Fatalf("items: %v %v", kinds, titles)
	}
	for _, it := range items {
		if it.Text == "Morgen" {
			t.Fatalf("tomorrow's appointment listed: %v", titles)
		}
	}
	if !items[0].Past || items[len(items)-1].Kind != "deadline" {
		t.Fatalf("order/past: %v %+v", kinds, items)
	}
}
