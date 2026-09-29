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

var renames = map[string][]rename{}

// Upgrade returns config with a type's old keys under their current
// names, and whether anything moved. A current key wins over an old one.
func Upgrade(key string, config map[string]any) (map[string]any, bool) {
	var out map[string]any
	for _, r := range renamesOf(key) {
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

func renamesOf(key string) []rename {
	if r := registry[key].Renames; r != nil {
		return r
	}
	return renames[key]
}

// RenamedTypes lists the types with renamed keys, sorted.
func RenamedTypes() []string {
	var out []string
	for key := range registry {
		if renamesOf(key) != nil {
			out = append(out, key)
		}
	}
	return slices.Sorted(slices.Values(out))
}
