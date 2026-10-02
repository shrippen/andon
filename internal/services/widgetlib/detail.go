package widgetlib

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"andon/internal/model"
)

// Detail dialogs of tiles. The frame (head, close, loading, reload) is the
// same for every type; a type adds a loader here and a template
// "details/<type>" that picks one of Kante's layouts:
//
//	GET /details/{id} ─► boards.Detail (right check) ─► LoadDetail
//	    ─► detailKinds[type](widget) ─► DetailDialog{Head, Body} ─► template "details/<type>"
//
// A new popup: a loader, an entry in detailKinds, a template.

// ErrNoDetail: the tile has no detail dialog (type without one, or a link
// without status check).
var ErrNoDetail = errors.New("tile without detail dialog")

// DetailDialog is one dialog: the head every type shares and the type's data.
type DetailDialog struct {
	Type string
	Head DetailHead
	Body any
}

// DetailHead is the dialog's head: monogram and name, a state as Kante
// names it (ok, warn, bad, off) with its catalog key, and actions.
type DetailHead struct {
	Title    string
	Sub      string
	State    string // "" = none
	StateKey string
	Actions  []DetailAction
}

// DetailAction is a button in the head: a link (Href) or a forced refresh
// of the tile's data, after which the dialog reloads.
type DetailAction struct {
	LabelKey string
	Href     string
	Refresh  bool
	Primary  bool // at most one per dialog (Kante: one primary per view)
}

// detailLoader reads a type's dialog; the caller checked the right on the
// widget.
type detailLoader func(ctx context.Context, d *sql.DB, widget *model.Widget, now time.Time) (*DetailDialog, error)

var detailKinds = map[string]detailLoader{
	"link": loadLinkDetail,
}

// HasDetail tells whether tiles of a type open a detail dialog.
func HasDetail(widgetType string) bool {
	_, ok := detailKinds[widgetType]
	return ok
}

// LoadDetail loads a placed tile's dialog; the caller checked the
// viewer's right on the widget.
func LoadDetail(ctx context.Context, d *sql.DB, widget *model.Widget, now time.Time) (*DetailDialog, error) {
	load, ok := detailKinds[widget.Type]
	if !ok {
		return nil, ErrNoDetail
	}
	return load(ctx, d, widget, now)
}
