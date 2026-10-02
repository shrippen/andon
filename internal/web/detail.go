package web

import (
	"errors"
	"net/http"

	"andon/internal/services/boards"
	"andon/internal/services/widgetlib"
)

// detailTemplate is the prefix of a type's dialog template:
// "details/link" draws the link tile's dialog.
const detailTemplate = "details/"

// handleDetail answers a tile's detail dialog (opened by [data-details],
// see andon.js). Every type shares the head ("detail_head"); its template
// draws the body in one of Kante's layouts.
func (d Deps) handleDetail(w http.ResponseWriter, r *http.Request) {
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

	dialog, err := boards.Detail(r.Context(), d.DB, ctx.Who, id)
	if errors.Is(err, widgetlib.ErrNoDetail) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, detailTemplate+dialog.Type, http.StatusOK, map[string]any{"Dialog": dialog, "D": dialog.Body, "PlacementID": id, "ThemeURL": ""})
}
