package detailacts

// Marks a dialog sets in the space's history, read back by rules and
// dialogs: a restore test, a deadline handed in.

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	data "andon/internal/repos/data"
	"andon/internal/rules"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
)

// ErrBadMark: the mark names nothing the dialog offers.
var ErrBadMark = errors.New("detail.bad_mark")

func init() {
	register("backups", "restore_tested", restoreTested)
}

// restoreTested notes that a backup system was restored as a test; the
// rule backups.restore_untested is quiet for it until the next quarter.
func restoreTested(_ context.Context, d *sql.DB, who *access.Principal, c Call) error {
	system := c.Form.Get("system")
	if !slices.Contains(rules.BackupSystems, enums.ServiceType(system)) {
		return ErrBadMark
	}
	if err := needSpace(who, c.Widget.SpaceID, enums.RightUse); err != nil {
		return err
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		if err := data.AddEvent(tx, c.Widget.SpaceID, data.Event{At: c.Now, Kind: metrics.EventRestore, Subject: system}); err != nil {
			return err
		}
		return auditsvc.Log(tx, &who.UserID, "restore.tested", system, c.IP, nil)
	})
}
