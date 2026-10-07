package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestESPLines: offline first, then behind, then by name.
func TestESPLines(t *testing.T) {
	lines := espLines(sources.DemoESPHome(time.Now()))
	if lines[0].Friendly != "Teichpumpe" || lines[0].State != "bad" || !lines[1].Behind || lines[2].State != "ok" {
		t.Fatalf("lines: %+v", lines)
	}
}

// TestGainOf: first to last recorded value, gaps skipped.
func TestGainOf(t *testing.T) {
	if g := gainOf([]float64{Gap, 400, Gap, 406}); g != 6 {
		t.Fatalf("gain %d", g)
	}
	if g := gainOf([]float64{Gap, 400}); g != 0 {
		t.Fatalf("one value %d", g)
	}
}
