package widgets

import "testing"

// TestPromValueQuery: the hours reach the source as a number it reads.
func TestPromValueQuery(t *testing.T) {
	cfg, _ := Decode("prometheus_value", map[string]any{"query": "up", "hours": "7d"})
	q := registry["prometheus_value"].Queries(cfg)
	if len(q) != 1 || q[0].Params["hours"] != 168.0 || q[0].Params["query"] != "up" {
		t.Fatalf("query %+v", q)
	}
}
