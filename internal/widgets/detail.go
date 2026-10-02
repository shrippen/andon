package widgets

import (
	"time"

	"andon/internal/enums"
)

// Detail dialogs: a tile type may declare Detail next to View. It gets the
// same results as View plus what a dialog needs beyond the tile:
//
//	results[HistorySlot]     *metrics.History  stored daily values and events of the space
//	results[DetailHintsSlot] []DetailHint      open hints of the tile's services
//	results[TileViewSlot]    map[string]any    the tile's own View, to reuse its numbers
//	results[DetailItemSlot]  string            the list entry the viewer picked, "" = none
//
// and returns the head (state, actions) and the body: a *DetailBody of
// blocks (detailbody.go), or data for a template "details/<key>".

// Slots a Detail function finds besides the tile's own.
const (
	DetailHintsSlot = "detail_hints"
	TileViewSlot    = "tile_view"
	DetailItemSlot  = "detail_item"
)

// DetailFunc shapes results into a dialog.
type DetailFunc func(cfg any, results map[string]any, ctx ViewCtx) DetailView

// DetailView is a dialog as its type draws it: Body is a *DetailBody
// (template "details/body") or the data of the type's own template.
type DetailView struct {
	Head DetailHead
	Body any
}

// DetailHead is the dialog's head: name (set by the loader from the
// tile), a line under it, a state as Kante names it (ok, warn, bad, off)
// with its catalog key, and actions.
type DetailHead struct {
	Title     string
	Sub       string
	State     string // "" = none
	StateKey  string
	StateArgs map[string]any // catalog parameters of StateKey ({"n": 3})
	Actions   []DetailAction
}

// DetailAction is a button in the head: a link (Href) or a forced refresh
// of the tile's data, after which the dialog reloads.
type DetailAction struct {
	LabelKey string
	Href     string
	Refresh  bool
	Primary  bool // at most one per dialog (Kante: one primary per view)
}

// DetailHint is an open hint as a dialog lists it.
type DetailHint struct {
	ID        int64
	Rule      string
	Severity  enums.Severity
	Title     string
	Why       string
	FirstSeen time.Time
	Due       string // "2026-10-02", "" = none
}
