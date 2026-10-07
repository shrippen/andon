package widgets

import (
	"testing"
	"time"
)

// Dialogs with times of day name their zone: the today tile its own,
// the calendar Andon's clock zone, Dawarich's places the server's.
func TestDetailZones(t *testing.T) {
	if z := todayDetail(TodayConfig{Timezone: "Asia/Tokyo"}, map[string]any{}, ViewCtx{}).Head.Zone; z != "Asia/Tokyo" {
		t.Fatalf("today: %q", z)
	}
	if z := calendarDetail(CalendarConfig{}, map[string]any{}, ViewCtx{}).Head.Zone; z != clockZone().String() {
		t.Fatalf("calendar: %q", z)
	}
	if z := zoned(DetailView{}, time.Local).Head.Zone; z != "Local" {
		t.Fatalf("local: %q", z)
	}
}
