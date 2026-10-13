package vatcheck

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/assist"
)

// prefKey is the user pref holding the check's state:
//
//	{"conn": 3,                                 the Invoice Ninja last used
//	 "guesses": {"3": {"Kx9": {…}}}}            the model's readings
const prefKey = "vat_check"

type state struct {
	Conn    int64                              `json:"conn,omitempty"`
	Guesses map[string]map[string]assist.Guess `json:"guesses,omitempty"`
}

func stateOf(raw any) state {
	var s state
	if blob, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(blob, &s)
	}
	if s.Guesses == nil {
		s.Guesses = map[string]map[string]assist.Guess{}
	}
	return s
}

func (s state) prefValue() map[string]any {
	blob, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(blob, &out)
	return out
}

func connKey(id int64) string { return strconv.FormatInt(id, 10) }

func (s state) guesses(conn int64) map[string]assist.Guess { return s.Guesses[connKey(conn)] }

func loadState(d *sql.DB, who *access.Principal) (state, error) {
	u, err := users.Get(d, who.UserID)
	if err != nil || u == nil {
		return stateOf(nil), err
	}
	return stateOf(u.Prefs[prefKey]), nil
}

// saveState changes the stored state in one transaction.
func saveState(d *sql.DB, who *access.Principal, change func(*state)) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil {
			return err
		}
		if u == nil {
			return sql.ErrNoRows
		}
		s := stateOf(u.Prefs[prefKey])
		change(&s)
		if u.Prefs == nil {
			u.Prefs = map[string]any{}
		}
		u.Prefs[prefKey] = s.prefValue()
		return users.Update(tx, u)
	})
}

func equalFold(a, b string) bool {
	return a != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
