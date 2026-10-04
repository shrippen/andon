package widgets_test

import (
	"testing"

	"andon/internal/widgets"
)

// TestCalendarLogin: a calendar's user and password join its address as
// Basic-Auth login; a slot without address stays unused.
func TestCalendarLogin(t *testing.T) {
	cfg, _ := widgets.Decode("calendar", map[string]any{
		"ical_url": "https://h/a", "ical_user": "anna", "ical_pass": "p@ss",
		"ical_url_2":  "https://h/b",
		"ical_user_3": "ghost", "ical_pass_3": "x",
	})
	c := cfg.(widgets.CalendarConfig)

	if c.URL != "https://anna:p%40ss@h/a" {
		t.Errorf("first: %q", c.URL)
	}
	if c.More[0] != "https://h/b" || c.More[1] != "" {
		t.Errorf("more: %q", c.More)
	}
}
