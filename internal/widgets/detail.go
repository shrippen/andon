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
	HintWorkSlot    = "hint_work" // the picked hint's work (HintWork), hint dialogs
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
	// Zone is the time zone of the dialog's times of day (IANA name,
	// "Local" for the server's); the dialog names it where the browser
	// is in another one. "" = no times.
	Zone string
}

// zoned marks a dialog's times as being in loc.
func zoned(v DetailView, loc *time.Location) DetailView {
	v.Head.Zone = loc.String()
	return v
}

// todayZone is the today tile's own time zone, UTC if unknown.
func todayZone(cfg TodayConfig) *time.Location {
	if loc, err := time.LoadLocation(cfg.Timezone); err == nil {
		return loc
	}
	return time.UTC
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

// HintWork is a picked hint's state of work for its dialog: who has it,
// what happened, who may take it.
type HintWork struct {
	ID         int64
	Title, Why string
	Assignee   string
	AssigneeID int64
	Work       string
	History    []HintStep
	People     []FormOption // who may take it over: id, name
}

// HintStep is one entry of a hint's history.
type HintStep struct {
	At         time.Time
	Kind, Note string
	Actor      string
}
