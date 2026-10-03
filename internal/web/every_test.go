package web

import (
	"fmt"
	"testing"
)

// TestEverySpreads: tiles of one board poll at slightly different
// periods, so their refreshes drift apart instead of all firing at once.
func TestEverySpreads(t *testing.T) {
	const seconds = 300
	low, high := seconds-seconds/refreshShare/2, seconds+seconds/refreshShare/2

	seen := map[string]bool{}
	for range 200 {
		got := every(seconds)
		var s int
		if _, err := fmt.Sscanf(got, "every %ds", &s); err != nil {
			t.Fatalf("bad trigger %q: %v", got, err)
		}
		if s < low || s > high {
			t.Fatalf("%q outside [%d, %d] s", got, low, high)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatal("expected varying periods")
	}
}
