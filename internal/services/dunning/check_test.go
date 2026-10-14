package dunning

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// A reminder passes for a due invoice at its next stage only.
func TestCheck(t *testing.T) {
	due := metrics.DunningCase{NinjaOpenInvoice: metrics.NinjaOpenInvoice{NinjaInvoice: sources.NinjaInvoice{Key: "i1", Number: "R-1"}, Client: "Werft"}, Next: 2}
	b := book{cases: []metrics.DunningCase{due}}
	step := func(key, stage string) workbench.Step {
		return workbench.Step{Case: key, Action: ActRemind, Fields: map[string]string{FieldStage: stage, FieldBody: " Hallo "}}
	}
	for _, c := range []struct {
		step workbench.Step
		want workbench.Problem
	}{
		{step("i1", "2"), workbench.ProblemNone},
		{step("i1", "1"), ProblemStage},
		{step("i9", "2"), ProblemGone},
	} {
		it, problem := b.Check(&c.step)
		if problem != c.want {
			t.Errorf("%+v: %q, want %q", c.step, problem, c.want)
		}
		if problem == workbench.ProblemNone && it.Body != "Hallo" {
			t.Errorf("body: %q", it.Body)
		}
	}
}
