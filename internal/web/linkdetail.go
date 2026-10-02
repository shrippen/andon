package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"andon/internal/services/boards"
	"andon/internal/services/linkstatus"
	"andon/internal/services/widgetlib"
)

// handleLinkDetail answers a link tile's detail dialog (opened by the
// icon next to its uptime strip).
func (d Deps) handleLinkDetail(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Viewer(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	detail, err := boards.LinkDetail(r.Context(), d.DB, ctx.Who, id)
	if errors.Is(err, widgetlib.ErrNoStatus) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "link_detail", http.StatusOK, map[string]any{"D": detail, "PlacementID": id, "ThemeURL": ""})
}

// stackPath draws checks per day as columns: successful ones below,
// failed ones on top, a low mark for days without checks. The viewBox
// is one unit per day and stackHeight high.
type stackPath struct {
	OK, Fail, None string
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
	return stackPath{OK: ok.String(), Fail: fail.String(), None: none.String()}
}

// column is one rectangle of a path: "M x y h.7 v h h-.7z".
func column(x string, y, h float64) string {
	return "M" + x + " " + strconv.FormatFloat(y, 'f', 1, 64) + "h" + stackColumn + "v" + strconv.FormatFloat(h, 'f', 1, 64) + "h-" + stackColumn + "z"
}

// msChart is the response time line in a msWidth × msHeight viewBox,
// with the slow line at msGoal.
type msChart struct {
	Line  string
	GoalY string
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
	}
	goal := msHeight - msGoal/scale*msHeight
	return msChart{Line: line.String(), GoalY: strconv.FormatFloat(goal, 'f', 1, 64)}
}

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
