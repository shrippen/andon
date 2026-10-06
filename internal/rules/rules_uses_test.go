package rules_test

import (
	"slices"
	"testing"

	"andon/internal/caps"
	"andon/internal/rules"
)

// Every declared use names a domain its holder declares, and belongs to
// a registered rule.
func TestUsesDeclared(t *testing.T) {
	for _, scope := range []string{rules.Cross, rules.Deadlines} {
		for _, spec := range rules.ForScope(scope) {
			for _, u := range rules.UsesOf(spec.ID) {
				if !slices.ContainsFunc(caps.Declared(u.Holder), func(c caps.Cap) bool { return c.Domain == u.Domain }) {
					t.Errorf("%s uses undeclared %v", spec.ID, u)
				}
			}
		}
	}
}

// A failed Dawarich keeps the hints of a rule on Kimai's places: the
// plugin's places keep Dawarich's areas (caps.Refs).
func TestNeedsFollowRefs(t *testing.T) {
	need := rules.NeedsOf("geo.plugin_missing")
	if !slices.Contains(need, "kimai") || !slices.Contains(need, "dawarich") {
		t.Fatalf("needs %v", need)
	}
	var spec rules.Spec
	for _, s := range rules.ForScope(rules.Cross) {
		if s.ID == "cross.expense_unrecorded" {
			spec = s
		}
	}
	env := rules.Env{Datasets: map[string]any{rules.FailedDataset: []rules.Failed{{Service: "mail"}}}}
	if !spec.Incomplete(env) {
		t.Fatal("a failed mailbox would read as unrecorded expenses")
	}
	env.Datasets[rules.FailedDataset] = []rules.Failed{{Service: "docker"}}
	if spec.Incomplete(env) {
		t.Fatal("docker does not matter here")
	}
}
