package data

import (
	"strconv"
	"time"

	"andon/internal/db"
	"andon/internal/model"
	"andon/internal/repos/misc"
)

// AddHookEvent stores one pushed event.
func AddHookEvent(q db.Queryer, e *model.HookEvent) error {
	res, err := q.Exec("INSERT INTO hook_events (connection_id, event, subject, at) VALUES (?,?,?,?)",
		e.ConnectionID, e.Event, e.Subject, db.TimeStr(e.At))
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	return err
}

// HookEvents returns a connection's events since a time, oldest first.
func HookEvents(q db.Queryer, connID int64, since time.Time) ([]*model.HookEvent, error) {
	rows, err := q.Query("SELECT id, connection_id, event, subject, at FROM hook_events WHERE connection_id = ? AND at >= ? ORDER BY at, id",
		connID, db.TimeStr(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.HookEvent
	for rows.Next() {
		var e model.HookEvent
		var at string
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.Event, &e.Subject, &at); err != nil {
			return nil, err
		}
		if e.At, err = db.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// PruneHookEvents deletes events older than a time.
func PruneHookEvents(q db.Queryer, before time.Time) error {
	_, err := q.Exec("DELETE FROM hook_events WHERE at < ?", db.TimeStr(before))
	return err
}

// hookStateKey stores a connection's last pushed state (instance
// settings): {"at": RFC 3339, "state": {…}}.
const hookStateKey = "hookstate."

// SetHookState replaces a connection's pushed state.
func SetHookState(q db.Queryer, connID int64, state map[string]any, at time.Time) error {
	return misc.SetSetting(q, hookStateKey+strconv.FormatInt(connID, 10), map[string]any{"at": db.TimeStr(at), "state": state})
}

// HookState is a connection's last pushed state; nil and zero if none.
func HookState(q db.Queryer, connID int64) (map[string]any, time.Time, error) {
	raw, err := misc.Setting(q, hookStateKey+strconv.FormatInt(connID, 10))
	if err != nil {
		return nil, time.Time{}, err
	}
	state, _ := raw["state"].(map[string]any)
	text, _ := raw["at"].(string)
	at, _ := db.ParseTime(text)
	return state, at, nil
}
