package widgets

import (
	"reflect"
	"testing"
)

// TestSourcesIgnoreCase: service keys are lower case; "Kimai" must
// still pick Kimai's hints on every tile that filters by source.
func TestSourcesIgnoreCase(t *testing.T) {
	raw := map[string]any{"sources": []any{"Kimai", "TrueNAS"}}
	want := []string{"kimai", "truenas"}
	for _, key := range []string{"hints", "expiries", "updates", "status_light"} {
		cfg, _ := Decode(key, raw)
		got := reflect.ValueOf(cfg).FieldByName("Sources").Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: sources %v, want %v", key, got, want)
		}
	}
}
