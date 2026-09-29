package widgets

import (
	"reflect"
	"testing"
)

// TestRenamedKeys: a config saved under an old key reads like one under
// the new key, in the decoder and in the form.
func TestRenamedKeys(t *testing.T) {
	cases := []struct {
		key      string
		old, new map[string]any
	}{
		{"komodo_stacks", map[string]any{"only_issues": true}, map[string]any{"only_problems": true}},
		{"tailscale", map[string]any{"only_trouble": true}, map[string]any{"only_problems": true}},
		{"github", map[string]any{"only_red": true}, map[string]any{"only_problems": true}},
		{"authentik_logins", map[string]any{"only_failures": true, "span": "24h"}, map[string]any{"only_problems": true, "period": "24h"}},
		{"conn_health", map[string]any{"only_shaky": true}, map[string]any{"only_problems": true}},
		{"exposure", map[string]any{"only_open": true}, map[string]any{"only_problems": true}},
		{"hint_noise", map[string]any{"period": "90"}, map[string]any{"days": "90"}},
		{"hint_trend", map[string]any{"period": "14"}, map[string]any{"days": "14"}},
		{"rate_trend", map[string]any{"target": 95.0}, map[string]any{"target_value": 95.0}},
		{"payment_days", map[string]any{"target": 14.0}, map[string]any{"target_days": 14.0}},
		{"list", map[string]any{"columns": "2"}, map[string]any{"two_columns": true}},
		{"hints", map[string]any{"by_value": true, "levels": false, "buttons": true},
			map[string]any{"sort": "value", "show_levels": false, "show_buttons": true}},
	}
	for _, c := range cases {
		got, _ := Decode(c.key, c.old)
		want, _ := Decode(c.key, c.new)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: old %+v, new %+v", c.key, got, want)
		}
		if def, _ := Decode(c.key, nil); reflect.DeepEqual(want, def) {
			t.Errorf("%s: new keys have no effect", c.key)
		}

		upgraded, changed := Upgrade(c.key, c.old)
		if !changed || !reflect.DeepEqual(upgraded, c.new) {
			t.Errorf("%s: upgraded %v", c.key, upgraded)
		}
		for _, v := range FormValues(c.key, c.old) {
			if n, ok := c.new[v.Key]; ok && v.Text != textOf(n) {
				t.Errorf("%s: form %s = %q, want %v", c.key, v.Key, v.Text, n)
			}
		}
	}
	if _, changed := Upgrade("hints", map[string]any{"sort": "age"}); changed {
		t.Error("current config changed")
	}
}
