package widgets

// Calm: a tile has nothing to do. Each type that can tell says so from
// its finished view; with the frame option "only_issues" the board then
// hides the tile until something needs attention.

import "reflect"

// IsCalm tells whether a type's view reports nothing to do.
func IsCalm(key string, view map[string]any) bool {
	check := registry[key].Calm
	return check != nil && view != nil && check(view)
}

func isZero(v any) bool {
	switch n := v.(type) {
	case int:
		return n == 0
	case float64:
		return n == 0
	}
	return false
}

// lenOf is the length of a slice in a view, whatever its element type.
func lenOf(v any) int {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		return rv.Len()
	}
	return 0
}
