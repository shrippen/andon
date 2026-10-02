package widgets

import "math"

// Charts of detail dialogs as data; web draws them with Kante's .chart
// (template "detail_chart"). A gap in a line is NaN:
//
//	Graph{Kind: GraphLine, Series: []Series{{Values: temps, Class: "s5"}}, Goal: 45, HasGoal: true}

// GraphKind is how a chart draws its values.
type GraphKind string

const (
	GraphLine GraphKind = "line" // one line per series
	GraphCols GraphKind = "cols" // columns of the first series, the second as last period
)

// Graph is one chart of a dialog.
type Graph struct {
	Kind       GraphKind
	Series     []Series
	Goal       float64
	HasGoal    bool
	GoalDanger bool     // the goal is a limit (red), not a target (yellow)
	Lo, Hi     float64  // value range; both 0 = from the values
	Mark       int      // index of the "now" line, -1 = none
	Ticks      []string // labels under the chart, spread evenly
	States     []string // cols: a Kante state per column (ok, warn, bad, off), "" = series colour
}

// Series is one line or one row of columns.
type Series struct {
	Values []float64
	Class  string // Kante data colour s1 … s6
	Label  string // legend
}

// Gap marks a missing value in a line.
var Gap = math.NaN()

// LineGraph is a line chart without goal or mark.
func LineGraph(series ...Series) Graph {
	return Graph{Kind: GraphLine, Series: series, Mark: -1}
}

// ColGraph is a column chart of one series.
func ColGraph(values []float64, class string) Graph {
	return Graph{Kind: GraphCols, Series: []Series{{Values: values, Class: class}}, Mark: -1}
}

// Strip is a row of day states for Kante's svg.uptime: name, one state
// per day (ok, warn, bad, off), a value on the right.
type Strip struct {
	Name   string
	States []string
	Value  string
}
