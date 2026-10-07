package widgets

import (
	"math"
	"time"

	"andon/internal/metrics"
)

// Parts shared by the types' Detail functions.

// hintsBlock lists the open hints of the tile's services; none: no block.
func hintsBlock(results map[string]any) []Block {
	list, _ := results[DetailHintsSlot].([]DetailHint)
	if len(list) == 0 {
		return nil
	}
	return []Block{{Kind: BlockHints, Label: T("detail.hints"), Data: list}}
}

// historyOf is the space's stored history, nil if none.
func historyOf(results map[string]any) *metrics.History {
	h, _ := results[HistorySlot].(*metrics.History)
	return h
}

// dailySeries is a stored series over the last n days, oldest first; a
// day without a value is a Gap:
//
//	"truenas.pool.tank.used", 30 days ─► [0.70, 0.70, Gap, 0.71, …]
func dailySeries(h *metrics.History, key string, now time.Time, n int) []float64 {
	return onDays(h.SeriesOf(key), now, n)
}

// onDays places points on the last n days, oldest first; a day without
// one is a Gap.
func onDays(points []metrics.Point, now time.Time, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = Gap
	}
	first := metrics.Today(now).AddDate(0, 0, -(n - 1))
	for _, p := range points {
		i := int(metrics.Today(p.Day).Sub(first).Hours() / hoursPerDay)
		if i >= 0 && i < n {
			out[i] = p.Value
		}
	}
	return out
}

// scaled multiplies every value but gaps: shares 0–1 as percent.
func scaled(values []float64, f float64) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = v * f
	}
	return out
}

// hasValues tells whether a series has a line to draw: two values at least.
func hasValues(values []float64) bool {
	n := 0
	for _, v := range values {
		if !math.IsNaN(v) {
			n++
		}
	}
	return n >= minPoints
}

// minPoints is what a line needs to be one.
const minPoints = 2

// spanTicks labels a span of n days: its first day and "today".
func spanTicks(now time.Time, n int) []any {
	return []any{Day(metrics.Today(now).AddDate(0, 0, -(n - 1))), Txt("detail.today")}
}

// dayLabels are the x labels of a span of n days ending today, one per
// day: the hover names the day of each value.
func dayLabels(now time.Time, n int) []any {
	out := make([]any, n)
	first := metrics.Today(now).AddDate(0, 0, -(n - 1))
	for i := range out {
		out[i] = Day(first.AddDate(0, 0, i))
	}
	return out
}

// monthLabels are the x labels of n months ending with last's month:
// monthLabels(Sep 2026, 2, "01/2006") → "08/2026", "09/2026".
func monthLabels(last time.Time, n int, layout string) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = metrics.AddMonths(last, i-(n-1)).Format(layout)
	}
	return out
}

// percentScale turns shares into percent.
const percentScale = 100

// agoOf is the time since t as a typed value ("vor 5 h"), "–" for zero.
func agoOf(t time.Time) any {
	if t.IsZero() {
		return "–"
	}
	return map[string]any{"$ago": t.Format(time.RFC3339)}
}

// pairOf puts two blocks side by side; one stays alone.
func pairOf(blocks []Block) []Block {
	if len(blocks) < 2 {
		return blocks
	}
	return []Block{{Kind: BlockPair, Data: blocks[:2]}, {Kind: BlockPair, Data: blocks[2:]}}[:1+min(1, len(blocks[2:]))]
}

// stateRank orders rows: trouble first.
func stateRank(s string) int {
	switch s {
	case "bad":
		return 0
	case "warn":
		return 1
	case "info":
		return 2
	case "ok":
		return 3
	}
	return 4
}

// tierState maps a tile tier (red, yellow, green, mid, bad, ok) to a cell state.
func tierState(tier string) string {
	switch tier {
	case "red", "bad":
		return "bad"
	case "yellow", "mid":
		return "warn"
	case "green", "ok":
		return "ok"
	}
	return ""
}

// tierIf is tier when cond holds, else alt.
func tierIf(cond bool, tier, alt string) string {
	if cond {
		return tier
	}
	return alt
}

// stateIf is state when cond holds, else none.
func stateIf(cond bool, state string) string {
	return tierIf(cond, state, "")
}

// dataClass is Kante's data colour of the i-th series (s1 … s6).
func dataClass(i int) string {
	return "s" + string(rune('1'+i%6))
}

// filled replaces gaps by the value before them (sparklines draw no gaps);
// leading gaps take the first value.
func filled(values []float64) []float64 {
	out := make([]float64, 0, len(values))
	last := math.NaN()
	for _, v := range values {
		if !math.IsNaN(v) {
			last = v
		}
		if !math.IsNaN(last) {
			out = append(out, last)
		}
	}
	return out
}

// asF reads a number of a view map.
func asF(v any) float64 {
	f, _ := v.(float64)
	return f
}

// meanSeries is the mean per index over series, gaps skipped.
func meanSeries(all [][]float64) []float64 {
	if len(all) == 0 {
		return nil
	}
	out := make([]float64, len(all[0]))
	for i := range out {
		sum, n := 0.0, 0
		for _, s := range all {
			if i < len(s) && !math.IsNaN(s[i]) {
				sum, n = sum+s[i], n+1
			}
		}
		out[i] = Gap
		if n > 0 {
			out[i] = sum / float64(n)
		}
	}
	return out
}

// dataDetail hands a Detail function the typed dataset of the "data"
// query; while it is missing the dialog shows the hints only.
func dataDetail[C, D any](f func(cfg C, data D, ctx ViewCtx, results map[string]any) DetailView) func(C, map[string]any, ViewCtx) DetailView {
	return func(cfg C, results map[string]any, ctx ViewCtx) DetailView {
		data, ok := results[dataName].(D)
		if !ok {
			return DetailView{Body: &DetailBody{Blocks: hintsBlock(results)}}
		}
		return f(cfg, data, ctx, results)
	}
}

// dayOf is a date as a typed value, "–" for none.
func dayOf(t time.Time) any {
	if t.IsZero() {
		return "–"
	}
	return Day(t)
}

// pickedItem is the list entry the viewer picked, "" for none.
func pickedItem(results map[string]any) string {
	item, _ := results[DetailItemSlot].(string)
	return item
}

// pickIndex is the index of the picked entry among keys, 0 when none
// matches: the first entry is shown until one is picked.
func pickIndex(results map[string]any, keys []string) int {
	item := pickedItem(results)
	for i, k := range keys {
		if k == item {
			return i
		}
	}
	return 0
}

// openName is the result name of a dialog's own data (DetailQueries).
const openName = "open"

// openQuery is a dialog's fetch on open from the tile's connection:
// DetailQueries: openQuery[DockerConfig]("docker.detail").
func openQuery[C any](source string) func(C) []Query {
	return func(C) []Query { return []Query{{Name: openName, Source: source, Conn: ConnWidget}} }
}
