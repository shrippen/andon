package web

import (
	"strconv"
	"strings"
	"time"

	"andon/internal/services/linkstatus"
)

// stackPath draws checks per day as columns: successful ones below,
// failed ones on top, a low mark for days without checks. The viewBox
// is one unit per day and stackHeight high.
type stackPath struct {
	OK, Fail, None string
	Most           int // checks of the busiest day, the top of the axis
}

const (
	stackHeight = 100
	stackNone   = 4 // height of the mark of a day without checks
	stackColumn = ".7"
	stackInset  = .15 // gap left of a column, so columns stand apart
)

// stackPaths scales the columns to the busiest day:
//
//	ok 144 fail 0 → full aqua column; ok 106 fail 38 → aqua with red on top
func stackPaths(days []linkstatus.Day) stackPath {
	most := 1
	for _, d := range days {
		most = max(most, d.OK+d.Fail)
	}

	var ok, fail, none strings.Builder
	for i, d := range days {
		x := strconv.FormatFloat(float64(i)+stackInset, 'f', 2, 64)
		if d.State == linkstatus.BarNone {
			none.WriteString(column(x, stackHeight-stackNone, stackNone))
			continue
		}
		okH := float64(d.OK) / float64(most) * stackHeight
		failH := float64(d.Fail) / float64(most) * stackHeight
		if okH > 0 {
			ok.WriteString(column(x, stackHeight-okH, okH))
		}
		if failH > 0 {
			fail.WriteString(column(x, stackHeight-okH-failH, failH))
		}
	}
	return stackPath{OK: ok.String(), Fail: fail.String(), None: none.String(), Most: most}
}

// Half is the middle of the axis.
func (s stackPath) Half() float64 { return float64(s.Most) / 2 }

// column is one rectangle of a path: "M x y h.7 v h h-.7z".
func column(x string, y, h float64) string {
	return "M" + x + " " + strconv.FormatFloat(y, 'f', 1, 64) + "h" + stackColumn + "v" + strconv.FormatFloat(h, 'f', 1, 64) + "h-" + stackColumn + "z"
}

// msChart is the response time line in a msWidth × msHeight viewBox,
// with the slow line at msGoal.
type msChart struct {
	Line   string
	GoalY  string
	Top    float64     // ms at the top edge, the axis runs from 0
	Values []float64   // ms of each drawn point, for the hover read-out
	Days   []time.Time // their days
}

const (
	msWidth   = 600
	msHeight  = 140
	msGoal    = 1000 // ms that count as slow
	msHeadway = 1.15 // room above the highest point
)

// msChartOf draws the daily average; days without an answer leave a gap
// in x but no point.
func msChartOf(days []linkstatus.Day) msChart {
	top := msGoal
	for _, d := range days {
		top = max(top, d.AvgMs)
	}
	scale := float64(top) * msHeadway
	step := float64(msWidth) / float64(max(len(days), 1))

	var line strings.Builder
	var values []float64
	var drawn []time.Time
	for i, d := range days {
		if d.AvgMs == 0 {
			continue
		}
		cmd := "L"
		if line.Len() == 0 {
			cmd = "M"
		}
		y := msHeight - float64(d.AvgMs)/scale*msHeight
		line.WriteString(cmd + strconv.FormatFloat((float64(i)+.5)*step, 'f', 1, 64) + " " + strconv.FormatFloat(y, 'f', 1, 64))
		values, drawn = append(values, float64(d.AvgMs)), append(drawn, d.Day)
	}
	goal := msHeight - msGoal/scale*msHeight
	return msChart{Line: line.String(), GoalY: strconv.FormatFloat(goal, 'f', 1, 64), Top: scale, Values: values, Days: drawn}
}

// Half is the middle of the axis.
func (m msChart) Half() float64 { return m.Top / 2 }

// msX is the x of day i on the response time chart (the chosen day's mark).
func msX(i, days int) string {
	return strconv.FormatFloat((float64(i)+.5)*float64(msWidth)/float64(max(days, 1)), 'f', 1, 64)
}

// kanteState names a day's state as Kante does (ok, warn, bad, off).
func kanteState(s linkstatus.BarState) string {
	for _, st := range uptimeStates {
		if st.bar == s {
			return st.kante
		}
	}
	return ""
}
