package analysis

import (
	"slices"
	"testing"
	"time"

	"andon/internal/rules"
)

// A cross rule whose inputs failed this run keeps its hints: it is left
// out of the rules whose missing findings resolve. Otherwise one failed
// Komodo fetch resolved every "unused service" hint and the next run
// opened them again.
func TestApplyKeepsHintsOfIncompleteRules(t *testing.T) {
	all := rules.AllRules()
	spec := all[slices.IndexFunc(all, func(s rules.Spec) bool { return s.ID == "system.unused_service" })]
	env := rules.Env{Today: time.Now(), Settings: map[string]any{},
		Datasets: map[string]any{rules.FailedDataset: []rules.Failed{{Service: "komodo", Name: "Komodo"}}}}
	_, ids := apply([]rules.Spec{spec}, nil, env)
	if slices.Contains(ids, "system.unused_service") {
		t.Fatalf("ids: %v", ids)
	}

	env.Datasets = map[string]any{}
	if _, ids = apply([]rules.Spec{spec}, nil, env); !slices.Contains(ids, "system.unused_service") {
		t.Fatalf("complete run: %v", ids)
	}
}
