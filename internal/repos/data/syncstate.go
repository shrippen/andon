package data

import (
	"strconv"

	"andon/internal/db"
	"andon/internal/repos/misc"
)

// syncStateKey stores what an outbound connection (Homelable) remembers
// between syncs, instance settings: IDs it created and the last log.
const syncStateKey = "syncstate."

// SetSyncState replaces a connection's sync state.
func SetSyncState(q db.Queryer, connID int64, state map[string]any) error {
	return misc.SetSetting(q, syncStateKey+strconv.FormatInt(connID, 10), state)
}

// SyncState is a connection's sync state; empty if none.
func SyncState(q db.Queryer, connID int64) (map[string]any, error) {
	return misc.Setting(q, syncStateKey+strconv.FormatInt(connID, 10))
}
