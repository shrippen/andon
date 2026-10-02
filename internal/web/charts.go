package web

import (
	"andon/internal/enums"
	"math"
	"strconv"
	"strings"

	"andon/internal/widgets"
)

// Geometry of detail dialog charts (widgets.Graph) in Kante's .chart: a
// chartWidth × chartHeight viewBox stretched to the block, the template
// "detail_chart" draws it.
//
//	values 0…100, Lo/Hi 0 ─► range from the values with chartHeadway on top
//	line   ─► path "M0 120.0L6.0 118.5…" per series, a gap (NaN) starts a new "M"
//	cols   ─► one bar per value, the second series as last period (dashed)

const (
	chartWidth   = 600
	chartHeight  = 140
	chartPad     = 8    // space above and below the drawn values
	chartHeadway = 1.08 // room above the highest value of columns
	chartBarFill = .68  // share of a column's slot the bar fills
)

// chartGeom is a chart ready to draw.
type chartGeom struct {
	W, H  int
	Grid  []string // y of the three grid lines
	Lines []geomLine
	Bars  []geomBar
	Goal  string // y, "" = none
	Mark  string // x of the "now" line, "" = none
}

type geomLine struct{ D, Class string }

type geomBar struct {
	X, Y, W, H string
	Class      string
	Style      string // data-style, "" = none
}

// stateColour is the Kante role of a column's state.
var stateColour = map[string]string{"ok": "--c:var(--aqua)", "warn": "--c:var(--warn)", "bad": "--c:var(--danger)", "off": "--c:var(--bg2)", "info": "--c:var(--cyan)"}

func fmtF(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// chartRange is the value range: given, else from the values (columns
// start at 0).
func chartRange(c widgets.Graph) (lo, hi float64) {
	if c.Lo != 0 || c.Hi != 0 {
		return c.Lo, c.Hi
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range c.Series {
		for _, v := range s.Values {
			if !math.IsNaN(v) {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if c.HasGoal {
		lo, hi = min(lo, c.Goal), max(hi, c.Goal)
	}
	if math.IsInf(lo, 0) {
		return 0, 1
	}
	if c.Kind == widgets.GraphCols {
		return 0, max(hi*chartHeadway, 1)
	}
	if hi == lo {
		hi = lo + 1
	}
	return lo, hi
}

func geomOf(c widgets.Graph) chartGeom {
	g := chartGeom{W: chartWidth, H: chartHeight}
	for _, f := range []float64{.25, .5, .75} {
		g.Grid = append(g.Grid, fmtF(chartHeight*f))
	}
	lo, hi := chartRange(c)
	y := func(v float64) float64 { return chartHeight - chartPad - (v-lo)/(hi-lo)*(chartHeight-2*chartPad) }
	if c.HasGoal {
		g.Goal = fmtF(y(c.Goal))
	}

	n := 0
	for _, s := range c.Series {
		n = max(n, len(s.Values))
	}
	if n == 0 {
		return g
	}

	if c.Kind == widgets.GraphCols {
		slot := float64(chartWidth) / float64(n)
		base := y(lo)
		bar := func(i int, v float64, class, style string) geomBar {
			top := y(v)
			return geomBar{X: fmtF(float64(i)*slot + slot*(1-chartBarFill)/2), Y: fmtF(top), W: fmtF(slot * chartBarFill), H: fmtF(max(base-top, 0)), Class: class, Style: style}
		}
		if len(c.Series) > 1 {
			for i, v := range c.Series[1].Values {
				if !math.IsNaN(v) {
					g.Bars = append(g.Bars, bar(i, v, "bar is-prev", ""))
				}
			}
		}
		for i, v := range c.Series[0].Values {
			if math.IsNaN(v) {
				continue
			}
			class, style := "bar "+c.Series[0].Class, ""
			if i < len(c.States) && c.States[i] != "" {
				class, style = "bar c", stateColour[c.States[i]]
			}
			g.Bars = append(g.Bars, bar(i, v, class, style))
		}
		if c.Mark >= 0 && c.Mark < n {
			g.Mark = fmtF((float64(c.Mark) + .5) * slot)
		}
		return g
	}

	step := float64(chartWidth) / float64(max(n-1, 1))
	for _, s := range c.Series {
		var d strings.Builder
		pen := "M"
		for i, v := range s.Values {
			if math.IsNaN(v) {
				pen = "M"
				continue
			}
			d.WriteString(pen + fmtF(float64(i)*step) + " " + fmtF(y(v)))
			pen = "L"
		}
		g.Lines = append(g.Lines, geomLine{D: d.String(), Class: "line " + s.Class})
	}
	if c.Mark >= 0 && c.Mark < n {
		g.Mark = fmtF(float64(c.Mark) * step)
	}
	return g
}

// statePath is one state's columns of a Kante svg.uptime strip.
type statePath struct{ State, D string }

// stripOrder fixes the drawing order of the states.
var stripOrder = []string{"ok", "warn", "bad", "off"}

// stripPaths draws one column per day in a "0 0 <days> 1" viewBox:
//
//	["ok","bad"] ─► ok "M0.1 0h.8v1h-.8z", bad "M1.1 0h.8v1h-.8z"
func stripPaths(states []string) []statePath {
	by := map[string]*strings.Builder{}
	for i, s := range states {
		if by[s] == nil {
			by[s] = &strings.Builder{}
		}
		by[s].WriteString("M" + fmtF(float64(i)+.1) + " 0h.8v1h-.8z")
	}
	var out []statePath
	for _, s := range stripOrder {
		if b := by[s]; b != nil {
			out = append(out, statePath{State: s, D: b.String()})
		}
	}
	return out
}

// sparkPath is a Kante .spark line in a 100 × 24 viewBox.
func sparkPath(values []float64) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	var d strings.Builder
	for i, v := range values {
		pen := "L"
		if i == 0 {
			pen = "M"
		}
		d.WriteString(pen + fmtF(float64(i)*100/float64(max(len(values)-1, 1))) + " " + fmtF(22-(v-lo)/span*20))
	}
	return d.String()
}

// sevTier is the Kante tier of a hint's severity: critical red, warning
// yellow, the rest cyan.
func sevTier(s enums.Severity) string {
	switch {
	case s >= enums.SeverityCritical:
		return "red"
	case s >= enums.SeverityWarn:
		return "yellow"
	}
	return "cyan"
}

// stateVar is the Kante colour of a row's state light: "ok" → "var(--aqua)".
func stateVar(s string) string {
	if v, ok := stateColour[s]; ok {
		return v[len("--c:"):]
	}
	return "var(--fg3)"
}

// seriesVar is the data colour of a series class: "s3" → "var(--d3)".
func seriesVar(class string) string {
	if len(class) == 2 && class[0] == 's' {
		return "var(--d" + class[1:] + ")"
	}
	return "var(--fg3)"
}

// numCol tells whether column i is a number column (right-aligned).
func numCol(cols []int, i int) bool {
	for _, c := range cols {
		if c == i {
			return true
		}
	}
	return false
}

// graphLegend tells whether a graph names its series.
func graphLegend(g widgets.Graph) bool {
	for _, s := range g.Series {
		if s.Label != "" {
			return true
		}
	}
	return false
}

// pctOf is part of total in whole percent: 3 of 4 → 75.
func pctOf(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}
