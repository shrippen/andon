package receipts

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strconv"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
)

// prefKey is the user pref holding the receipts page's state:
//
//	{"ninja": 3, "paperless": 5,                     the chosen connections
//	 "ignored": {"n3": ["Kx9"], "p5": [44]},          hidden per connection
//	 "reasons": {"n3/Kx9": "private"},                why, where given
//	 "aliases": [["hetzner", "hetzner online gmbh"]], learned vendor names
//	 "backfilled": ["3-5"]}                           pairs learned from once
const prefKey = "receipts"

// state is the pref, decoded.
type state struct {
	Ninja      int64               `json:"ninja,omitempty"`
	Paperless  int64               `json:"paperless,omitempty"`
	Ignored    map[string][]string `json:"ignored,omitempty"`
	Reasons    map[string]Reason   `json:"reasons,omitempty"`
	Aliases    Aliases             `json:"aliases,omitempty"`
	Backfilled []string            `json:"backfilled,omitempty"`
}

// stateOf decodes the stored pref; anything broken reads as empty.
func stateOf(raw any) state {
	var s state
	blob, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(blob, &s)
	}
	if m, ok := raw.(map[string]any); ok {
		s.Aliases = aliasesOf(m["aliases"])
	}
	if s.Ignored == nil {
		s.Ignored = map[string][]string{}
	}
	if s.Reasons == nil {
		s.Reasons = map[string]Reason{}
	}
	return s
}

func (s state) prefValue() map[string]any {
	blob, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(blob, &out)
	return out
}

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

// Kind is what an ignore entry hides.
type Kind string

const (
	KindExpense Kind = "expense"
	KindDoc     Kind = "doc"
)

// ignoreKey is the ignored list of one connection: "n3" (expenses of
// Invoice Ninja 3), "p5" (scans of Paperless 5).
func ignoreKey(kind Kind, connID int64) string {
	prefix := "n"
	if kind == KindDoc {
		prefix = "p"
	}
	return prefix + strconv.FormatInt(connID, 10)
}

func (s state) ignored(kind Kind, connID int64) []string {
	return s.Ignored[ignoreKey(kind, connID)]
}

// Reason says why something is hidden; "" = not said.
type Reason string

const (
	ReasonNone      Reason = ""
	ReasonPrivate   Reason = "private"
	ReasonNoReceipt Reason = "no_receipt"
	ReasonDuplicate Reason = "duplicate"
)

// Reasons in the order the page offers them.
var Reasons = []Reason{ReasonPrivate, ReasonNoReceipt, ReasonDuplicate, ReasonNone}

func (s state) reason(kind Kind, connID int64, id string) Reason {
	return s.Reasons[ignoreKey(kind, connID)+"/"+id]
}

// Hide says whether an entry gets hidden or shown again.
type Hide bool

const (
	Hidden Hide = true
	Shown  Hide = false
)

func (s *state) setIgnored(kind Kind, connID int64, id string, hide Hide, why Reason) {
	key := ignoreKey(kind, connID)
	list := slices.DeleteFunc(slices.Clone(s.Ignored[key]), func(x string) bool { return x == id })
	delete(s.Reasons, key+"/"+id)
	if hide == Hidden {
		list = append(list, id)
		if why != ReasonNone {
			s.Reasons[key+"/"+id] = why
		}
	}
	if len(list) == 0 {
		delete(s.Ignored, key)
		return
	}
	s.Ignored[key] = list
}

func pairKey(ninja, paperless int64) string {
	return strconv.FormatInt(ninja, 10) + "-" + strconv.FormatInt(paperless, 10)
}
