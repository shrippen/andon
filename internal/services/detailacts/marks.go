package detailacts

// Marks a dialog sets in the space's history, read back by rules and
// dialogs: a restore test, a deadline handed in.

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
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
	register("deadlines", "deadline_filed", deadlineFiled)
}

// deadlineKey is "kind:period:year", as metrics.DeadlineKey writes it.
var deadlineKey = regexp.MustCompile(`^[a-z_]+:[^:]{1,20}:\d{4}$`)

// deadlineFiled notes a tax deadline as handed in.
func deadlineFiled(_ context.Context, d *sql.DB, who *access.Principal, c Call) error {
	key := c.Form.Get("key")
	if !deadlineKey.MatchString(key) {
		return ErrBadMark
	}
	if err := needSpace(who, c.Widget.SpaceID, enums.RightUse); err != nil {
		return err
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		if err := data.AddEvent(tx, c.Widget.SpaceID, data.Event{At: c.Now, Kind: metrics.EventFiled, Subject: key}); err != nil {
			return err
		}
		return auditsvc.Log(tx, &who.UserID, "deadline.filed", key, c.IP, nil)
	})
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
