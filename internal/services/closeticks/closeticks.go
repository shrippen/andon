// Package closeticks keeps the month-close steps a user ticked by hand,
// for steps Andon cannot see (the VAT return) or wants confirmed.
//
//	tile ──POST──► Toggle: a month_close tile the user sees? ──► user prefs
//	               "close_ticks": {"2026-08": ["vat"]}
package closeticks

import (
	"database/sql"
	"errors"
	"regexp"
	"slices"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/widgets"
)

// WidgetType is the widget key the ticks belong to.
const WidgetType = "month_close"

const keepMonths = 24 // older months are dropped on the next tick

// ErrNotClose means the placement is not a month-close tile.
var ErrNotClose = errors.New("close.not_close")

var monthPattern = regexp.MustCompile(`^\d{4}-\d{2}$`)

// Toggle ticks or unticks one step of a month behind a tile.
func Toggle(d *sql.DB, who *access.Principal, placementID int64, month, step string) error {
	w, err := boards.PlacedWidget(d, who, placementID)
	if err != nil {
		return err
	}
	if w.Type != WidgetType || !monthPattern.MatchString(month) || step == "" {
		return ErrNotClose
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil {
			return err
		}
		if u == nil {
			return sql.ErrNoRows
		}
		ticks := widgets.CloseTicksOf(u.Prefs[widgets.CloseTicksPref])
		if i := slices.Index(ticks[month], step); i >= 0 {
			ticks[month] = slices.Delete(ticks[month], i, i+1)
		} else {
			ticks[month] = append(ticks[month], step)
		}
		months := make([]string, 0, len(ticks))
		for m := range ticks {
			months = append(months, m)
		}
		slices.Sort(months)
		for len(months) > keepMonths {
			delete(ticks, months[0])
			months = months[1:]
		}
		if u.Prefs == nil {
			u.Prefs = map[string]any{}
		}
		u.Prefs[widgets.CloseTicksPref] = ticks
		return users.Update(tx, u)
	})
}
