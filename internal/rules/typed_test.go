package rules

import (
	"testing"

	"andon/internal/enums"
)

type typedData struct{ Level float64 }

type typedCfg struct {
	Warn float64 `json:"warn"`
	Tags []any   `json:"tags"`
}

// A typed rule gets its dataset and config as Go types: defaults become
// the settings form's fields, a space's values override them, and a
// dataset of the wrong type never reaches the rule.
func TestRegisterTyped(t *testing.T) {
	var seen typedCfg
	id := "test.typed"
	registerTyped(id, enums.ServiceType("test"), typedCfg{Warn: 5}, func(d *typedData, c typedCfg, _ Env) []Finding {
		seen = c
		if d.Level < c.Warn {
			return nil
		}
		return []Finding{{Fingerprint: "x"}}
	})
	t.Cleanup(func() { delete(registry, id) })

	spec := registry[id]
	if spec.Scope != "test" || spec.Defaults["warn"] != 5.0 || spec.Defaults[Enabled] != true {
		t.Fatalf("spec: %+v", spec)
	}
	cfg := Config(spec, map[string]any{"rules": map[string]any{id: map[string]any{"warn": 2.0}}})
	if got := spec.Run(&typedData{Level: 3}, cfg, Env{}); len(got) != 1 || seen.Warn != 2 {
		t.Fatalf("override: %v %+v", got, seen)
	}
	if got := spec.Run("wrong type", cfg, Env{}); got != nil {
		t.Fatalf("wrong dataset: %v", got)
	}
}
