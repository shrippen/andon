// Package clicks counts how often a link tile is opened from a board: a
// link nobody uses is a candidate to clear away, and the link's dialog
// shows the days.
//
//	board ──beacon POST /widget-fragments/{id}/click──► Count ──► samples
//	        link.clicks.<widget id>  (added up per day)
package clicks

import (
	"database/sql"
	"errors"
	"time"

	"andon/internal/db"
	"andon/internal/metrics"
	data "andon/internal/repos/data"
	"andon/internal/services/access"
	"andon/internal/services/boards"
)

// linkType is the widget type that counts.
const linkType = "link"

// ErrNotLink: only link tiles count clicks.
var ErrNotLink = errors.New("clicks.not_link")

// Count adds one click to the link tile behind a placement the viewer sees.
func Count(d *sql.DB, who *access.Principal, placementID int64, now time.Time) error {
	w, err := boards.PlacedWidget(d, who, placementID)
	if err != nil {
		return err
	}
	if w.Type != linkType {
		return ErrNotLink
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		return data.AddSamples(tx, w.SpaceID, 0, now.Format(time.DateOnly), map[string]float64{metrics.LinkClicksKey(w.ID): 1})
	})
}
