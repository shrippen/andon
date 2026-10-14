package vatcheck

import (
	"database/sql"
	"strconv"
	"strings"

	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/workbench"
)

// prefKey is the user pref holding the check's state:
//
//	{"scope": "v4",                             the Verbund last used (workbench.Scope)
//	 "guesses": {"3": {"Kx9": {…}}},            the model's readings
//	 "filed": {"3": {"2026-09": {"81": 1000}}}} figures marked as filed, per Ninja
const prefKey = "vat_check"

type state struct {
	Scope   string                                   `json:"scope,omitempty"`
	Guesses map[string]map[string]assist.Guess       `json:"guesses,omitempty"`
	Filed   map[string]map[string]map[string]float64 `json:"filed,omitempty"`
}

// normal gives a state read from the pref its maps.
func (s *state) normal() {
	if s.Guesses == nil {
		s.Guesses = map[string]map[string]assist.Guess{}
	}
	if s.Filed == nil {
		s.Filed = map[string]map[string]map[string]float64{}
	}
}

func connKey(id int64) string { return strconv.FormatInt(id, 10) }

func (s state) guesses(conn int64) map[string]assist.Guess { return s.Guesses[connKey(conn)] }

func loadState(d *sql.DB, who *access.Principal) (state, error) {
	s, err := workbench.LoadState[state](d, who, prefKey)
	s.normal()
	return s, err
}

// saveState changes the stored state in one transaction.
func saveState(d *sql.DB, who *access.Principal, change func(*state)) error {
	return workbench.SaveState(d, who, prefKey, func(s *state) {
		s.normal()
		change(s)
	})
}

func equalFold(a, b string) bool {
	return a != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
