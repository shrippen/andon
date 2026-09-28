package sources

import "testing"

// A failed disk names what failed: the attributes Scrutiny or SMART
// flag, with their raw value ("Reallocated Sectors Count 8").
func TestScrutinyFailingAttrs(t *testing.T) {
	details := map[string]any{
		"data": map[string]any{"smart_results": []any{map[string]any{"attrs": map[string]any{
			"5":   map[string]any{"attribute_id": 5.0, "raw_value": 8.0, "status": 4.0},
			"9":   map[string]any{"attribute_id": 9.0, "raw_value": 41000.0, "status": 0.0},
			"197": map[string]any{"attribute_id": 197.0, "raw_value": 2.0, "status": 2.0},
		}}}},
		"metadata": map[string]any{
			"5":   map[string]any{"display_name": "Reallocated Sectors Count"},
			"9":   map[string]any{"display_name": "Power-On Hours"},
			"197": map[string]any{"display_name": "Current Pending Sector Count"},
		},
	}
	got := failingAttrs(details)
	if got != "Reallocated Sectors Count 8, Current Pending Sector Count 2" {
		t.Fatalf("attrs: %q", got)
	}
}
