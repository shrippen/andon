package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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

// Open are the cases a page shows: key → the basis of its proposal, ""
// none (a reminder's stage, an image's tags). A case drafted on another
// basis is not shown: its fields followed a proposal since replaced.
type Open map[string]string

// BasisField prefixes the basis a draft keeps for its case (web:
// data-draft-basis): basis_<case>.
const BasisField = "basis_"

// LoadDraft reads the draft at place with only the cases still open on
// the same basis. The others stay stored: a list may miss a case for a
// moment (its service unreadable); Forget removes the written ones.
func LoadDraft(d *sql.DB, who *access.Principal, at Place, open Open) (Draft, error) {
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
		basis, ok := open[k]
		if ok && basisOf(v, k) == basis {
			out[k] = v
		}
	}
	return out, nil
}

// basisOf is the basis a case's fields were drafted on.
func basisOf(fields map[string][]string, k string) string {
	if v := fields[BasisField+k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// KeepDraft stores the cases given over the stored ones; a case without
// fields is removed.
func KeepDraft(d *sql.DB, who *access.Principal, at Place, cases Draft) error {
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
		return storeDraft(tx, who, at, stored)
	})
}

// Forget removes written cases from the drafts of the bench whose key
// starts like within's, e.g. "v4/": every period of the Verbund written
// in. A case key is a service's own id: another Verbund's same key is
// another case.
func Forget(d *sql.DB, who *access.Principal, within Place, cases []string) error {
	if len(cases) == 0 {
		return nil
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		drafts, err := users.Drafts(tx, who.UserID, within.Bench)
		if err != nil {
			return err
		}
		for key := range drafts {
			if !strings.HasPrefix(key, within.Key) {
				continue
			}
			at := Place{Bench: within.Bench, Key: key}
			stored, err := readDraft(tx, who, at)
			if err != nil {
				return err
			}
			n := len(stored)
			for _, k := range cases {
				delete(stored, k)
			}
			if len(stored) == n {
				continue
			}
			if err := storeDraft(tx, who, at, stored); err != nil {
				return err
			}
		}
		return nil
	})
}

func storeDraft(q db.Queryer, who *access.Principal, at Place, stored Draft) error {
	if len(stored) == 0 {
		return users.DropDraft(q, who.UserID, at.Bench, at.Key)
	}
	blob, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if len(blob) > draftBytes {
		return ErrDraftSize
	}
	return users.SetDraft(q, who.UserID, at.Bench, at.Key, string(blob))
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

// Drafted is what a bench's page carries of its draft: the open cases
// the user changed, and where the draft holds (web: data-draft-place).
type Drafted struct {
	Draft    Draft
	DraftKey string
	Bases    Open // each case's basis, for its row (web: data-draft-basis)
}

// PlaceOf is a bench's draft in a Verbund, per period where the bench
// has one ("" none): "v4/2026-Q3", "v4/".
func PlaceOf(bench, scope, period string) Place {
	return Place{Bench: bench, Key: scope + "/" + period}
}

// OpenDraft loads the draft at place for a page showing the open cases.
func OpenDraft(d *sql.DB, who *access.Principal, at Place, open Open) (Drafted, error) {
	draft, err := LoadDraft(d, who, at, open)
	return Drafted{Draft: draft, DraftKey: at.Key, Bases: open}, err
}

// Written are the cases Apply wrote without a failure.
func Written[P any](plan Plan[P], res Result) []string {
	var out []string
	for _, p := range plan.Steps {
		failed := slices.ContainsFunc(res.Failed, func(f Failure) bool { return f.Case == p.Case })
		if p.Problem == ProblemNone && !failed {
			out = append(out, p.Case)
		}
	}
	return out
}

// ForgetWritten drops what Apply wrote from the bench's drafts in the
// Verbund, every period of it.
func ForgetWritten[P any](d *sql.DB, who *access.Principal, bench, scope string, plan Plan[P], res Result) error {
	return Forget(d, who, PlaceOf(bench, scope, ""), Written(plan, res))
}

// Keys are cases without a basis, for OpenDraft.
func Keys[C any](cases []C, key func(C) string) Open {
	return Based(cases, key, func(C) string { return "" })
}

// Based are cases with the basis of their proposal, for OpenDraft.
func Based[C any](cases []C, key, basis func(C) string) Open {
	out := Open{}
	for _, c := range cases {
		out[key(c)] = basis(c)
	}
	return out
}
