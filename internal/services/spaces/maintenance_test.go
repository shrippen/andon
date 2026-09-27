package spaces_test

import (
	"testing"
	"time"

	"andon/internal/services/spaces"
)

// A window covers its listed connections (all when none listed) until
// it ends; after that it is gone.
func TestMaintenanceCovers(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	nas, kimai := int64(3), int64(7)
	settings := map[string]any{"maintenance": map[string]any{
		"until": now.Add(2 * time.Hour).Format(time.RFC3339), "connections": []any{float64(nas)}, "note": "Update",
	}}
	m := spaces.MaintenanceOf(settings)
	if !m.Covers(&nas, now) || m.Covers(&kimai, now) || m.Covers(nil, now) || m.Covers(&nas, now.Add(3*time.Hour)) {
		t.Fatalf("listed window: %+v", m)
	}

	whole := spaces.MaintenanceOf(map[string]any{"maintenance": map[string]any{"until": now.Add(time.Hour).Format(time.RFC3339)}})
	if !whole.Covers(&kimai, now) || !whole.Covers(nil, now) {
		t.Fatalf("whole space: %+v", whole)
	}
	if spaces.MaintenanceOf(nil).Covers(&nas, now) {
		t.Fatal("no window covers nothing")
	}
}

// The form's local time becomes a stored UTC instant; an empty date ends
// the window.
func TestParseMaintenance(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	form := map[string]string{"maint_until": "2026-09-28T14:00", "maint_note": " Update "}
	got := spaces.ParseMaintenance(func(k string) string { return form[k] }, []string{"3", "x", "7"}, berlin)
	if got["until"] != "2026-09-28T12:00:00Z" || got["note"] != "Update" || len(got["connections"].([]any)) != 2 {
		t.Fatalf("parsed: %+v", got)
	}
	if off := spaces.ParseMaintenance(func(string) string { return "" }, nil, berlin); off["until"] != "" {
		t.Fatalf("empty form: %+v", off)
	}
}
