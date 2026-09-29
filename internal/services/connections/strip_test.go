package connections

import "testing"

// TestStripFailPct: one failure in 500 fetches is not a clean strip.
func TestStripFailPct(t *testing.T) {
	cases := []struct{ ok, fail, want int }{{499, 1, 1}, {0, 0, 0}, {50, 50, 50}, {0, 3, 100}}
	for _, c := range cases {
		if got := stripFailPct(c.ok, c.fail); got != c.want {
			t.Errorf("ok %d fail %d: %d %%, want %d", c.ok, c.fail, got, c.want)
		}
	}
}
