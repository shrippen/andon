package workbench

import (
	"database/sql"
	"encoding/json"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
)

// A bench keeps its state (the connection last used, the model's
// readings …) in one user pref, as JSON under its key.

// LoadState reads the state kept under key; missing is the zero value.
func LoadState[T any](d *sql.DB, who *access.Principal, key string) (T, error) {
	var s T
	u, err := users.Get(d, who.UserID)
	if err != nil || u == nil {
		return s, err
	}
	return decode[T](u.Prefs[key]), nil
}

// SaveState changes the state kept under key in one transaction.
func SaveState[T any](d *sql.DB, who *access.Principal, key string, change func(*T)) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil {
			return err
		}
		if u == nil {
			return sql.ErrNoRows
		}
		s := decode[T](u.Prefs[key])
		change(&s)
		if u.Prefs == nil {
			u.Prefs = map[string]any{}
		}
		u.Prefs[key] = encode(s)
		return users.Update(tx, u)
	})
}

func decode[T any](raw any) T {
	var s T
	if blob, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(blob, &s)
	}
	return s
}

func encode[T any](s T) map[string]any {
	blob, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(blob, &out)
	return out
}
