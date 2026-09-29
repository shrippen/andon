package widgets

// Renamed config keys: one name per meaning across types, e.g. every
// "show only what needs attention" box is only_problems. Stored
// configs, exports and revisions may still hold the old names;
// Decode and the form read them through Upgrade:
//
//	komodo_stacks {"only_issues": true} → {"only_problems": true}
//	hints {"by_value": true}            → {"sort": "value"}

import (
	"maps"
	"slices"
)

// rename moves one key; value, if set, converts the old value and may
// drop it (ok false).
type rename struct {
	from, to string
	value    func(v any) (any, bool)
}

var renames = map[string][]rename{
	"komodo_stacks":    {{from: "only_issues", to: "only_problems"}},
	"tailscale":        {{from: "only_trouble", to: "only_problems"}},
	"github":           {{from: "only_red", to: "only_problems"}},
	"authentik_logins": {{from: "only_failures", to: "only_problems"}, {from: "span", to: "period"}},
	"conn_health":      {{from: "only_shaky", to: "only_problems"}},
	"exposure":         {{from: "only_open", to: "only_problems"}},
	"hint_noise":       {{from: "period", to: "days"}},
	"hint_trend":       {{from: "period", to: "days"}},
	"rate_trend":       {{from: "target", to: "target_value"}},
	"payment_days":     {{from: "target", to: "target_days"}},
	"list":             {{from: "columns", to: "two_columns", value: func(v any) (any, bool) { return v == "2", true }}},
	"hints": {
		{from: "by_value", to: "sort", value: func(v any) (any, bool) { return HintSortValue, asBool(v) }},
		{from: "levels", to: "show_levels"},
		{from: "buttons", to: "show_buttons"},
	},
}

// Upgrade returns config with a type's old keys under their current
// names, and whether anything moved. A current key wins over an old one.
func Upgrade(key string, config map[string]any) (map[string]any, bool) {
	var out map[string]any
	for _, r := range renames[key] {
		v, ok := config[r.from]
		if !ok {
			continue
		}
		if out == nil {
			out = maps.Clone(config)
		}
		delete(out, r.from)

		if _, taken := out[r.to]; taken {
			continue
		}
		if r.value != nil {
			if v, ok = r.value(v); !ok {
				continue
			}
		}
		out[r.to] = v
	}
	if out == nil {
		return config, false
	}
	return out, true
}

// RenamedTypes lists the types with renamed keys, sorted.
func RenamedTypes() []string {
	return slices.Sorted(maps.Keys(renames))
}
