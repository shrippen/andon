package rules

import (
	"encoding/json"

	"andon/internal/enums"
)

// registerTyped registers a rule that works on Go types instead of
// any and map[string]any:
//
//	registerTyped("glances.swap_high", enums.ServiceGlances, swapCfg{Percent: 80},
//		func(d *sources.GlancesResult, c swapCfg, env Env) []Finding { … })
//
// The json-tagged fields of C are the rule's settings; the value given
// holds their defaults. A dataset that is not a *D (nil for rules across
// services is fine) skips the rule.
func registerTyped[D any, C any](id string, scope enums.ServiceType, defaults C, fn func(*D, C, Env) []Finding) {
	Register(id, string(scope), settingsOf(defaults), func(raw any, cfg map[string]any, env Env) []Finding {
		data, ok := raw.(*D)
		if raw != nil && !ok {
			return nil
		}
		// Start from the defaults: a setting of the wrong type keeps its
		// default instead of silencing the rule.
		c := defaults
		if b, err := json.Marshal(cfg); err == nil {
			_ = json.Unmarshal(b, &c)
		}
		return fn(data, c, env)
	})
}

// settingsOf turns a config struct into the settings map the rule
// settings form and Config work with (numbers as float64).
func settingsOf(cfg any) map[string]any {
	out := map[string]any{}
	if b, err := json.Marshal(cfg); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// noSettings is the config of a rule without settings of its own.
type noSettings struct{}
