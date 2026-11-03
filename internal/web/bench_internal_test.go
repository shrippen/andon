package web

import (
	"net/url"
	"testing"
)

// TestBenchOnly: only=<case> reads that row alone, ticked or not; the
// other ticked rows stay out.
func TestBenchOnly(t *testing.T) {
	form := url.Values{"pick": {"a"}, "act_a": {"create"}, "act_b": {"skip"}, "vendor_b": {"Nordhost"}, "only": {"b"}}
	steps := benchSteps(form, []string{"vendor"})
	if len(steps) != 1 || steps[0].Case != "b" || steps[0].Field("vendor") != "Nordhost" {
		t.Fatalf("steps = %+v, want b alone", steps)
	}
}
