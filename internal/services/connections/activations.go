package connections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
)

// Fields of a template an activation remembers (see snapshotOf).
const (
	FieldURL       = "url"
	FieldVerifyTLS = "verify_tls"
	FieldOption    = "option" // Change.Key names the option
)

// Change is one value of a template that differs from the one its
// activation was made for: "url" https://a → https://b.
type Change struct {
	Field string
	Key   string // the option's name for FieldOption
	Was   string
	Now   string
}

// Activation is one holder's state on one template.
type Activation struct {
	Template    View
	Holder      model.Holder
	Active      bool // a login is stored and current
	Paused      bool // the template changed since it was activated
	NeedsSecret bool // paused on a new host: the old login is gone
	SecretAt    time.Time
	Changes     []Change
}

// Activations lists the templates h may activate, with h's state: a user
// sees every template they may use, a team the instance's templates (only
// its owners may ask).
func Activations(d *sql.DB, who *access.Principal, h model.Holder) ([]Activation, error) {
	if t := h.Team(); t > 0 && who.Teams[t] != enums.TeamOwner {
		return nil, access.ErrDenied
	}
	if u := h.User(); u > 0 && u != who.UserID {
		return nil, access.ErrDenied
	}
	all, err := Listing(d, who, enums.RightView)
	if err != nil {
		return nil, err
	}

	var out []Activation
	err = db.WithRead(d, func(tx *sql.Tx) error {
		for _, v := range all {
			if v.Mode != enums.CredentialPersonal || (h.Team() > 0 && v.Level != enums.SpaceInstance) {
				continue
			}
			a, err := activationOf(tx, v, h)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return nil
	})
	return out, err
}

func activationOf(tx *sql.Tx, v View, h model.Holder) (Activation, error) {
	a := Activation{Template: v, Holder: h}
	conn, err := content.Connection(tx, v.ID)
	if err != nil || conn == nil {
		return a, err
	}
	cred, err := content.Credential(tx, v.ID, h)
	if err != nil || cred == nil {
		return a, err
	}
	a.SecretAt = cred.SecretAt
	a.Paused = paused(conn, cred)
	a.Active = !a.Paused
	a.NeedsSecret = cred.SecretEnc == nil
	if a.Paused {
		a.Changes = changesOf(cred.Snapshot, snapshotOf(conn))
	}
	return a, nil
}

// changesOf lists what differs between the values an activation was made
// for (was) and the template's (now), options by name.
func changesOf(was, now map[string]any) []Change {
	var out []Change
	for _, f := range []string{FieldURL, FieldVerifyTLS} {
		if a, b := text(was[f]), text(now[f]); a != b {
			out = append(out, Change{Field: f, Was: a, Now: b})
		}
	}

	wasOpts, _ := was["options"].(map[string]any)
	nowOpts, _ := now["options"].(map[string]any)
	var keys []string
	for k := range wasOpts {
		keys = append(keys, k)
	}
	for k := range nowOpts {
		if _, seen := wasOpts[k]; !seen {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range slices.Compact(keys) {
		if a, b := text(wasOpts[k]), text(nowOpts[k]); a != b {
			out = append(out, Change{Field: FieldOption, Key: k, Was: a, Now: b})
		}
	}
	return out
}

// text shows a snapshot value: strings as they are, others as JSON, a
// missing one empty.
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprint(t)
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
