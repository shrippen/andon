package widgets

import "testing"

// A taller tile shows more entries: limit × rows, within the field's Max.
func TestForRows(t *testing.T) {
	cases := []struct {
		name   string
		typ    string
		config map[string]any
		rows   int
		want   any
	}{
		{"default doubled", "rss", map[string]any{}, 2, 16},
		{"own limit doubled", "rss", map[string]any{"limit": 5.0}, 2, 10},
		{"capped at max", "rss", map[string]any{"limit": 30.0}, 2, 50},
		{"one row unchanged", "rss", map[string]any{"limit": 5.0}, 1, 5.0},
		{"no limit field", "clock", map[string]any{}, 2, nil},
	}
	for _, c := range cases {
		got := ForRows(c.typ, c.config, c.rows)["limit"]
		if got != c.want {
			t.Errorf("%s: limit %v, want %v", c.name, got, c.want)
		}
	}

	// The stored config stays as it is.
	stored := map[string]any{"limit": 5.0}
	ForRows("rss", stored, 2)
	if stored["limit"] != 5.0 {
		t.Errorf("stored config changed: %v", stored)
	}
}
