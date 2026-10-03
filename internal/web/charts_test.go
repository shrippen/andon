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
