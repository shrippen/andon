// Package detailacts runs what a tile's detail dialog offers to do
// (widgets.Task.Do): mark something done, change a list in a service.
// Every action checks its own right; the dialog draws itself anew after.
//
//	dialog button ──POST /details/{id}/do/{act}──► Run: placement visible?
//	    ──► acts[type][act](…) ──► repos (marks) / outbound (services)
package detailacts

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/boards"
)

// ErrUnknownAct: the tile's type offers no such action.
var ErrUnknownAct = errors.New("detail.unknown_act")

// Call is one action as the dialog sent it.
type Call struct {
	Widget *model.Widget
	Form   url.Values // the button's fields
	IP     string
	Now    time.Time
}

// act does one thing a dialog offers.
type act func(ctx context.Context, d *sql.DB, who *access.Principal, c Call) error

// acts are the actions by widget type and name.
var acts = map[string]map[string]act{}

// register adds an action of a widget type.
func register(widgetType, name string, f act) {
	if acts[widgetType] == nil {
		acts[widgetType] = map[string]act{}
	}
	acts[widgetType][name] = f
}

// Run does action name of the tile behind a placement the viewer sees.
func Run(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64, name string, form url.Values, ip string) error {
	w, err := boards.PlacedWidget(d, who, placementID)
	if err != nil {
		return err
	}
	f, ok := acts[w.Type][name]
	if !ok {
		return ErrUnknownAct
	}
	return f(ctx, d, who, Call{Widget: w, Form: form, IP: ip, Now: time.Now().UTC()})
}

// needSpace checks the viewer's right in the tile's space: seeing a board
// is not enough to mark things for everyone in it.
func needSpace(who *access.Principal, spaceID int64, right enums.Right) error {
	ref, ok := who.Spaces[spaceID]
	if !ok {
		return access.ErrDenied
	}
	return access.Need(access.SpaceRight(who, &ref), right)
}
