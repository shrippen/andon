package hints

import (
	"database/sql"
	"strings"

	"andon/internal/db"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/spaces"
)

// Runbooks: a space's own instructions per rule, e.g. for
// borg.client_offline "1. ssh nas  2. systemctl restart borg". Every hint
// of the rule in that space shows them, before "Was tun?" asks the LLM.
//
//	space settings: {"runbooks": {"borg.client_offline": "1. …"}}

const (
	runbooksKey = "runbooks"
	runbookMax  = 4000
)

// RunbookView is a hint's runbook and whether the viewer may change it.
type RunbookView struct {
	Text    string
	CanEdit bool
}

// Runbook returns the runbook of a hint's rule in its space.
func Runbook(d *sql.DB, who *access.Principal, hintID int64) (RunbookView, error) {
	var out RunbookView
	err := db.WithRead(d, func(tx *sql.Tx) error {
		hint, err := reachable(tx, who, hintID)
		if err != nil {
			return err
		}
		sp, err := content.Space(tx, hint.SpaceID)
		if err != nil || sp == nil {
			return ErrNotFound
		}
		books, _ := sp.Settings[runbooksKey].(map[string]any)
		out.Text, _ = books[hint.Rule].(string)
		out.CanEdit = spaces.CanChange(tx, who, hint.SpaceID)
		return nil
	})
	return out, err
}

// SetRunbook stores the runbook of a hint's rule in its space; empty text
// removes it. Needs the right to change the space's settings.
func SetRunbook(d *sql.DB, who *access.Principal, hintID int64, text string) error {
	var spaceID int64
	var rule string
	books := map[string]any{}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		hint, err := reachable(tx, who, hintID)
		if err != nil {
			return err
		}
		sp, err := content.Space(tx, hint.SpaceID)
		if err != nil || sp == nil {
			return ErrNotFound
		}
		if old, ok := sp.Settings[runbooksKey].(map[string]any); ok {
			for k, v := range old {
				books[k] = v
			}
		}
		spaceID, rule = hint.SpaceID, hint.Rule
		return nil
	})
	if err != nil {
		return err
	}

	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > runbookMax {
		text = string(r[:runbookMax])
	}
	if text == "" {
		delete(books, rule)
	} else {
		books[rule] = text
	}
	return spaces.Update(d, who, spaceID, map[string]any{runbooksKey: books}, "")
}
