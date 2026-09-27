package widgets_test

import (
	"strings"
	"testing"

	"andon/internal/widgets"
)

// TestSparkOf: a line through every value, an area closed to the floor,
// the last point marked; too few values give none.
func TestSparkOf(t *testing.T) {
	if widgets.SparkOf([]float64{3}) != nil {
		t.Fatal("one value makes no line")
	}
	s := widgets.SparkOf([]float64{1, 3, 2})
	if s == nil || strings.Count(s.Line, "L") != 2 || !strings.HasSuffix(s.Area, "Z") {
		t.Fatalf("spark: %+v", s)
	}
	if s.EndX != float64(s.W) {
		t.Fatalf("end marker not at the right edge: %+v", s)
	}
	flat := widgets.SparkOf([]float64{5, 5})
	if flat == nil || flat.EndY <= 0 || flat.EndY >= float64(flat.H) {
		t.Fatalf("flat line off the canvas: %+v", flat)
	}
}
