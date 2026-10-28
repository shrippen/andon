package spaces_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/services/spaces"
)

// A threshold reads in the user's notation; a typo is an error, not a
// rule "> 0" that fires on every value.
func TestCustomThreshold(t *testing.T) {
	form := func(value string) func(string) string {
		f := map[string]string{"cr_count": "1", "cr.0.service": "sabnzbd", "cr.0.path": "Slots", "cr.0.op": ">", "cr.0.value": value}
		return func(k string) string { return f[k] }
	}
	threshold := func(value string, n enums.Notation) float64 {
		t.Helper()
		out, err := spaces.ParseCustomRules(form(value), n)
		if err != nil || len(out) != 1 {
			t.Fatalf("%q (%s): %v %v", value, n, out, err)
		}
		return out[0].(map[string]any)["value"].(float64)
	}
	if v := threshold("1,5", enums.NotationDE); v != 1.5 {
		t.Errorf("de: %v", v)
	}
	if v := threshold("1.5", enums.NotationEN); v != 1.5 {
		t.Errorf("en: %v", v)
	}
	if _, err := spaces.ParseCustomRules(form("18.40"), enums.NotationDE); err == nil {
		t.Error(`"18.40" (de) saved`)
	}
}
