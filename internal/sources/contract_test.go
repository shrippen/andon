package sources

import (
	"testing"
	"time"
)

// Kimai keeps the working time as user preferences in seconds per
// weekday; unset or zero everywhere means no contract.
func TestParseContract(t *testing.T) {
	me := map[string]any{"preferences": []any{
		map[string]any{"name": "work_monday", "value": "28800"},
		map[string]any{"name": "work_tuesday", "value": 28800.0},
		map[string]any{"name": "work_friday", "value": "14400"},
		map[string]any{"name": "timezone", "value": "Europe/Berlin"},
	}}
	c := parseContract(me)
	if c == nil || c.WeekMinutes() != 8*60+8*60+4*60 {
		t.Fatalf("contract: %+v", c)
	}
	friday := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if c.Minutes(friday) != 240 || c.Minutes(friday.AddDate(0, 0, 1)) != 0 {
		t.Fatalf("per day: %d %d", c.Minutes(friday), c.Minutes(friday.AddDate(0, 0, 1)))
	}
	if parseContract(map[string]any{"preferences": []any{}}) != nil {
		t.Fatal("no preferences must give no contract")
	}
}
