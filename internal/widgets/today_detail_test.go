package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestTodayDetailAllDay: all-day appointments go to the strip's all-day
// lane, not to a block at midnight; one running since yesterday night is
// drawn from 0 h to its end.
func TestTodayDetailAllDay(t *testing.T) {
	day := time.Now().UTC().Truncate(24 * time.Hour)
	cfg := decodeOf[TodayConfig]("today", map[string]any{"timezone": "UTC", "show_transit": false, "show_deadlines": false, "show_timer": false})
	results := map[string]any{peerCalendar: &sources.CalendarResult{Events: []sources.Event{
		{Start: day, End: day.AddDate(0, 0, 1), AllDay: true, Title: "Feiertag"},
		{Start: day.AddDate(0, 0, -2), End: day.AddDate(0, 0, 3), AllDay: true, Title: "Urlaub"},
		{Start: day.AddDate(0, 0, -1).Add(20 * time.Hour), End: day.Add(2 * time.Hour), Title: "Nachtschicht"},
		{Start: day.Add(10 * time.Hour), Title: "Call"},
	}}}
	strip := todayDetail(cfg, results, ViewCtx{}).Body.(*DetailBody).Blocks[0].Data.(DayStrip)

	if len(strip.AllDay) != 2 || strip.AllDay[0].Title != "Feiertag" || strip.AllDay[1].Title != "Urlaub" {
		t.Fatalf("all-day: %+v", strip.AllDay)
	}
	if len(strip.Events) != 2 || strip.Events[0] != (HourSpan{From: 0, To: 2, Colour: "cyan"}) || strip.Events[1].From != 10 {
		t.Fatalf("events: %+v", strip.Events)
	}
}
