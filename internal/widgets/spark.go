package widgets

import "fmt"

// Spark is a small line chart for a tile: the line and the last point,
// in a W×H box (see the "spark" template).
type Spark struct {
	Line       string
	W, H       int
	EndX, EndY float64
}

const (
	sparkW   = 120
	sparkH   = 28
	sparkPad = 3 // keeps the end marker inside the box
)

// SparkOf draws values left to right; nil for fewer than two.
func SparkOf(values []float64) *Spark {
	if len(values) < 2 {
		return nil
	}
	low, high := values[0], values[0]
	for _, v := range values {
		low, high = min(low, v), max(high, v)
	}
	span := high - low
	if span == 0 {
		span = 1
	}

	s := &Spark{W: sparkW, H: sparkH}
	step := float64(sparkW) / float64(len(values)-1)
	for i, v := range values {
		x := float64(i) * step
		y := sparkH - sparkPad - (v-low)/span*(sparkH-2*sparkPad)
		if high == low {
			y = sparkH / 2
		}
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		s.Line += fmt.Sprintf("%s%.1f,%.1f ", cmd, x, y)
		s.EndX, s.EndY = x, y
	}
	return s
}
