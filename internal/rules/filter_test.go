package rules

import "testing"

// Every rule can drop old cases and named ones: an offer open for 942
// days goes with max_age_days 365, "Heizung*" and "sonos" drop by name.
func TestFilterFindings(t *testing.T) {
	found := []Finding{
		{Fingerprint: "a", Params: map[string]any{"number": "2024/0013", "days": 942}},
		{Fingerprint: "b", Params: map[string]any{"number": "2026/0003", "days": 12}},
		{Fingerprint: "c", Params: map[string]any{"name": "Heizung rechts Batterie"}},
		{Fingerprint: "d", Params: map[string]any{"name": "Sonos Küche"}},
	}
	cfg := map[string]any{MaxAge: 365.0, Exclude: "Heizung*, sonos"}
	got := Filter(found, cfg)
	if len(got) != 1 || got[0].Fingerprint != "b" {
		t.Fatalf("kept: %+v", got)
	}
	if len(Filter(found, map[string]any{MaxAge: 0.0, Exclude: ""})) != 4 {
		t.Fatal("defaults drop findings")
	}
}
