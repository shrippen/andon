package web

import (
	"strconv"
	"strings"

	"andon/internal/services/linkstatus"
)

// uptimePath is one state's columns of Kante's uptime strip.
type uptimePath struct {
	State string // Kante state: ok, warn, bad, off
	D     string // SVG path, one column per day
}

// uptimeStates maps a day's state to Kante's, in drawing order.
var uptimeStates = []struct {
	bar   linkstatus.BarState
	kante string
}{
	{linkstatus.BarNone, "off"},
	{linkstatus.BarUp, "ok"},
	{linkstatus.BarPartial, "warn"},
	{linkstatus.BarDown, "bad"},
}

// uptimeColumn is one day's column in a viewBox one unit per day; the
// rest of the unit is the gap.
const uptimeColumn = " 0h.7v1h-.7z"

// uptimePaths draws a tile's days as one path per state, so the strip
// is a single SVG instead of one element per day:
//
//	days  off ok bad ok  →  off "M0 …"  ok "M1 …M3 …"  bad "M2 …"
func uptimePaths(u linkstatus.Uptime) []uptimePath {
	cols := map[linkstatus.BarState]*strings.Builder{}
	for i, bar := range u.Bars {
		b := cols[bar.State]
		if b == nil {
			b = &strings.Builder{}
			cols[bar.State] = b
		}
		b.WriteString("M" + strconv.Itoa(i) + uptimeColumn)
	}

	var out []uptimePath
	for _, s := range uptimeStates {
		b := cols[s.bar]
		if b == nil {
			continue
		}
		out = append(out, uptimePath{State: s.kante, D: b.String()})
	}
	return out
}
