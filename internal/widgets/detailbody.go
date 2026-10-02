package widgets

import "time"

// DetailBody is a dialog's content as blocks; the template "details/body"
// draws any of them, so most types need no template of their own. The
// layout follows from what is set, as in Kante:
//
//	Side set ─► record (facts left)      List set ─► list and detail
//	End set  ─► large view (column right) none    ─► plain (timeline, grid, wall, tasks)
//
// Values (Value, Meta, Name …) are plain text (names) or typed values that
// the template formats per locale: Money(12.5, "EUR"), Num(3.5, 1),
// Day(t), Txt("status.up").
type DetailBody struct {
	Line   []Fact // facts as one line under the head
	Side   []Fact // facts column on the left
	List   *ObjList
	Facts  []Kpi
	Blocks []Block
	Tabs   []Tab  // after Blocks; the first is shown
	End    []Fact // narrow column on the right (large view)
}

// Text is a catalog key with its parameters.
type Text struct {
	Key  string
	Args map[string]any
}

// T is a Text without parameters.
func T(key string) Text { return Text{Key: key} }

// Tab is a tab of the dialog with its own blocks.
type Tab struct {
	Label  Text
	Count  any // shown in the tab, nil = none
	Blocks []Block
}

// Fact is a label and a value; State colours the value (ok, warn, bad).
type Fact struct {
	Label Text
	Value any
	State string
}

// Kpi is a figure of the facts row; Tier is Kante's (green, yellow, red, cyan).
type Kpi struct {
	Value any
	Label Text
	Tier  string
}

// ObjList is the object list of a list-and-detail dialog; Sel is shown.
type ObjList struct {
	Label     Text
	Items     []LitRow
	Sel       int
	Title     any // the chosen object's name
	Sub       any
	State     string // Kante state of the chosen object
	StateText Text
}

// LitRow is a list row with a state light: name, meta right, state.
type LitRow struct {
	Name  any
	Meta  any
	State string // ok, warn, bad, off, info
}

// BlockKind is how a block draws its Data.
type BlockKind string

const (
	BlockGraph    BlockKind = "graph"    // Data Graph
	BlockTable    BlockKind = "table"    // Data Table
	BlockStrips   BlockKind = "strips"   // Data []Strip, Ticks
	BlockBars     BlockKind = "bars"     // Data []ShareBar
	BlockTimeline BlockKind = "timeline" // Data []Event
	BlockRows     BlockKind = "rows"     // Data []LitRow
	BlockStatus   BlockKind = "status"   // Data []LitRow as Kante .status cards
	BlockText     BlockKind = "text"     // Data any (text or typed value)
	BlockCode     BlockKind = "code"     // Data string, monospace
	BlockHints    BlockKind = "hints"    // Data []DetailHint
	BlockWall     BlockKind = "wall"     // Data []Card
	BlockTasks    BlockKind = "tasks"    // Data Tasks
	BlockPair     BlockKind = "pair"     // Data []Block, two columns
	BlockHeat     BlockKind = "heat"     // Data Heat
	BlockDay      BlockKind = "day"      // Data DayCard
	BlockChips    BlockKind = "chips"    // Data []string
)

// Block is one part of the main area: a label row (text left, Meta right)
// over its content.
type Block struct {
	Kind  BlockKind
	Label Text
	Meta  any
	Hero  bool  // the dialog's main chart: taller
	Ticks []any // labels under strips (text or typed values)
	Data  any
}

// Table is a Kante table; Num marks right-aligned number columns.
type Table struct {
	Head []Text
	Rows [][]Cell
	Num  []int
	Foot []Cell
}

// Cell is a table cell; State colours it (ok, warn, bad).
type Cell struct {
	Value any
	State string
}

// ShareBar is a share bar: name, 0–100, value, Kante tier (green, yellow, red).
type ShareBar struct {
	Name  any
	Pct   float64
	Value any
	Tier  string
}

// Event is a timeline entry (Kante .timeline); Tier green, yellow, red, cyan.
type Event struct {
	At    time.Time
	Title any
	Sub   any
	State any
	Tier  string
}

// Card is a figure of a wall: label, value, sparkline, line under it.
type Card struct {
	Label Text
	Value any
	Tier  string
	Spark []float64
	Sub   any
}

// Tasks is a progress line over tasks with an action each.
type Tasks struct {
	Done, Total int
	Label       Text
	Items       []Task
}

// Task is one row: text, meta, state light, an optional link.
type Task struct {
	Text   any
	Meta   any
	State  string // ok = done
	Action Text
	Href   string
}

// Heat is a grid of levels 0–4, Rows high (Kante .heat), labels under it.
type Heat struct {
	Rows   int
	Levels []int
	Ticks  []any
}

// DayCard is the chosen day or object in a sheet.
type DayCard struct {
	Title     any
	State     string
	StateText Text
	Kpis      []Kpi
	Sub       any
}

// Typed values, formatted per locale by i18n.Typed.

// Money is an amount in a currency.
func Money(v float64, currency string) map[string]any {
	return map[string]any{"$money": v, "currency": currency}
}

// Num is a number with digits after the separator.
func Num(v float64, digits int) map[string]any {
	return map[string]any{"$num": v, "digits": digits}
}

// Day is a date.
func Day(t time.Time) map[string]any { return map[string]any{"$day": t.Format(time.DateOnly)} }

// Txt is a catalog text.
func Txt(key string) map[string]any { return map[string]any{"$t": key} }

// NumU is a number with a unit: NumU(47, 0, "°C") → "47 °C".
func NumU(v float64, digits int, unit string) map[string]any {
	return map[string]any{"$num": v, "digits": digits, "unit": unit}
}

// DayS is a date given as "2026-09-16".
func DayS(day string) map[string]any { return map[string]any{"$day": day} }

// TxtA is a catalog text with parameters, given as pairs; values may be
// typed: TxtA("detail.days", "n", 3), TxtA("detail.per_year", "amount", Money(…)).
func TxtA(key string, kv ...any) map[string]any {
	args := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			args[k] = kv[i+1]
		}
	}
	return map[string]any{"$t": key, "args": args}
}

// Plain is a label that is no catalog text (a pool, a mount point).
func Plain(text string) Text {
	return Text{Key: "detail.plain", Args: map[string]any{"text": text}}
}
