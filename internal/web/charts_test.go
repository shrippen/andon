package web

import (
	"strings"
	"testing"

	"andon/internal/widgets"
)

// TestLineGap: a gap (NaN) lifts the pen, the next value starts a new path.
func TestLineGap(t *testing.T) {
	g := geomOf(widgets.LineGraph(widgets.Series{Values: []float64{1, 2, widgets.Gap, 4}, Class: "s1"}))
	if got := strings.Count(g.Lines[0].D, "M"); got != 2 {
		t.Fatalf("pen ups %d in %q", got, g.Lines[0].D)
	}
}

// TestColsLastPeriod: the second series draws dashed behind the first, a
// state colours its column.
func TestColsLastPeriod(t *testing.T) {
	c := widgets.ColGraph([]float64{2, 4}, "s4")
	c.Series = append(c.Series, widgets.Series{Values: []float64{3, 3}})
	c.States = []string{"", "bad"}
	g := geomOf(c)
	if len(g.Bars) != 4 || g.Bars[0].Class != "bar is-prev" || g.Bars[3].Colour != "danger" {
		t.Fatalf("bars %+v", g.Bars)
	}
}

// TestStripPaths: one path per state in a fixed order.
func TestStripPaths(t *testing.T) {
	got := stripPaths([]string{"bad", "ok", "ok"})
	if len(got) != 2 || got[0].State != "ok" || got[1].D != "M0.1 0h.8v1h-.8z" {
		t.Fatalf("%+v", got)
	}
}

// TestAxisAtGrid: the value axis names the three grid lines, top first;
// a small range gets decimals.
func TestAxisAtGrid(t *testing.T) {
	c := widgets.LineGraph(widgets.Series{Values: []float64{0, 100}, Class: "s1"})
	c.Lo, c.Hi = 0, 100
	g := geomOf(c)
	if len(g.Axis) != 3 || g.Axis[1] != 50 || g.Axis[0] <= g.Axis[1] || g.Axis[2] >= g.Axis[1] {
		t.Fatalf("axis %v", g.Axis)
	}
	if g.Prec != 0 {
		t.Fatalf("prec %d", g.Prec)
	}

	small := geomOf(widgets.LineGraph(widgets.Series{Values: []float64{0.5, 2}, Class: "s1"}))
	if small.Prec != 1 || small.Axis[2] <= 0.5 {
		t.Fatalf("small axis %v prec %d", small.Axis, small.Prec)
	}
}

// TestHoverValues: a line carries the values it draws (gaps left out), a
// bar its own.
func TestHoverValues(t *testing.T) {
	g := geomOf(widgets.LineGraph(widgets.Series{Values: []float64{1, widgets.Gap, 4}, Class: "s1"}))
	if v := g.Lines[0].Values; len(v) != 2 || v[1] != 4 {
		t.Fatalf("line values %v", v)
	}
	b := geomOf(widgets.ColGraph([]float64{2, 4}, "s1"))
	if b.Bars[1].Value != 4 {
		t.Fatalf("bar value %+v", b.Bars[1])
	}
}

// TestStripStates: the legend names the states the rows use, in drawing
// order.
func TestStripStates(t *testing.T) {
	got := stripStates([]widgets.Strip{{States: []string{"bad", "ok"}}, {States: []string{"off", "ok"}}})
	if strings.Join(got, ",") != "ok,bad,off" {
		t.Fatalf("states %v", got)
	}
}

// TestColsNiceAxis: columns get a round top, grid lines on its quarters:
// 142 → 160, 120, 80, 40.
func TestColsNiceAxis(t *testing.T) {
	g := geomOf(widgets.ColGraph([]float64{30, 142}, "s1"))
	if len(g.Axis) != 3 || g.Axis[0] != 120 || g.Axis[1] != 80 || g.Axis[2] != 40 {
		t.Fatalf("axis %v", g.Axis)
	}
}

// TestXTicks: labels per value spread five ticks over the x axis, the
// given end ticks ("today") stay; without labels the ticks are as given.
func TestXTicks(t *testing.T) {
	c := widgets.ColGraph(make([]float64, 9), "s1")
	for i := range 9 {
		c.Labels = append(c.Labels, string(rune('a'+i)))
	}
	c.Ticks = []any{"start", "today"}
	got := geomOf(c).Ticks
	if len(got) != 5 || got[0] != "start" || got[1] != "c" || got[2] != "e" || got[4] != "today" {
		t.Fatalf("ticks %v", got)
	}

	c.Labels = nil
	if got := geomOf(c).Ticks; len(got) != 2 {
		t.Fatalf("plain ticks %v", got)
	}
}

// TestHoverLabels: a bar carries its x label, the line hover the labels
// of the first series' drawn points (gaps left out).
func TestHoverLabels(t *testing.T) {
	b := widgets.ColGraph([]float64{2, 4}, "s1")
	b.Labels = []any{"Mo", "Di"}
	if got := geomOf(b).Bars[1].Label; got != "Di" {
		t.Fatalf("bar label %v", got)
	}

	l := widgets.LineGraph(widgets.Series{Values: []float64{1, widgets.Gap, 4}, Class: "s1"})
	l.Labels = []any{"a", "b", "c"}
	if got := geomOf(l).Labels; len(got) != 2 || got[1] != "c" {
		t.Fatalf("line labels %v", got)
	}
}
