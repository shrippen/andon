package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"andon/internal/db"
	"andon/internal/repos/users"
	"andon/internal/services/access"
)

// Draft is a bench's unfinished work: per case the values of the form
// fields the user changed, as the form sends them:
//
//	{"Kx9": {"pick": ["Kx9"], "act_Kx9": ["create"], "vendor_Kx9": ["Nordhost"]}}
//
// The page keeps a case on every change and puts it back on load, so a
// closed page loses nothing; a case no longer open drops out.
type Draft map[string]map[string][]string

// Place is where a draft holds: a bench, e.g. "vat", and its Verbund
// and period, e.g. "v4/2026-Q3".
type Place struct {
	Bench, Key string
}

// draftBytes caps a draft's JSON: a few hundred cases with their fields.
const draftBytes = 1 << 20

// ErrDraftSize means a draft grew past draftBytes.
var ErrDraftSize = errors.New("workbench.err_draft_size")

// LoadDraft reads the draft at place with only the cases still open;
// the ones closed meanwhile (written, archived) are dropped for good.
func LoadDraft(d *sql.DB, who *access.Principal, at Place, open []string) (Draft, error) {
	var stored Draft
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		stored, err = readDraft(tx, who, at)
		return err
	})
	if err != nil {
		return nil, err
	}

	out := Draft{}
	for k, v := range stored {
		if slices.Contains(open, k) {
			out[k] = v
		}
	}
	if len(out) == len(stored) {
		return out, nil
	}
	return out, KeepDraft(d, who, at, nil, func(k string) bool { return !slices.Contains(open, k) })
}

// KeepDraft stores the cases given over the stored ones; a case without
// fields is removed, and so is every case drop names.
func KeepDraft(d *sql.DB, who *access.Principal, at Place, cases Draft, drop func(string) bool) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		stored, err := readDraft(tx, who, at)
		if err != nil {
			return err
		}
		for k, v := range cases {
			stored[k] = v
			if len(v) == 0 {
				delete(stored, k)
			}
		}
		for k := range stored {
			if drop != nil && drop(k) {
				delete(stored, k)
			}
		}
		if len(stored) == 0 {
			return users.DropDraft(tx, who.UserID, at.Bench, at.Key)
		}

		blob, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		if len(blob) > draftBytes {
			return ErrDraftSize
		}
		return users.SetDraft(tx, who.UserID, at.Bench, at.Key, string(blob))
	})
}

// DropDraft discards the draft at place.
func DropDraft(d *sql.DB, who *access.Principal, at Place) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		return users.DropDraft(tx, who.UserID, at.Bench, at.Key)
	})
}

func readDraft(q db.Queryer, who *access.Principal, at Place) (Draft, error) {
	text, err := users.Draft(q, who.UserID, at.Bench, at.Key)
	if err != nil {
		return nil, err
	}
	out := Draft{}
	if err := db.FromJSON(text, &out); err != nil {
		return Draft{}, nil // unreadable: start afresh
	}
	return out, nil
}
