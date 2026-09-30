package porting_test

import (
	"database/sql"
	"errors"
	"testing"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/accounts"
	"andon/internal/services/porting"
	"andon/internal/services/teams"
)

// TestImportNeedsManageToReplace: a team editor may add boards and
// tiles by import, but not wipe the space, change its settings or add
// connections; each of those needs MANAGE elsewhere too.
func TestImportNeedsManageToReplace(t *testing.T) {
	d := setup(t)
	var adminID int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		u, err := accounts.Create(tx, "admin@x.de", "A", nil, enums.RoleAdmin, enums.LocaleDE, "")
		adminID = u.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	adminWho, _ := access.Load(d, adminID)
	editor, _ := user(t, d, "editor@x.de")
	teamID, err := teams.Create(d, adminWho, "IT", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.SetMember(d, adminWho, teamID, editor.UserID, enums.TeamEditor, ""); err != nil {
		t.Fatal(err)
	}
	if err := teams.SetMember(d, adminWho, teamID, adminID, enums.TeamOwner, ""); err != nil {
		t.Fatal(err)
	}
	adminWho, _ = access.Load(d, adminID)
	editor, _ = access.Load(d, editor.UserID)
	space, _ := content.TeamSpace(d, teamID)

	for name, doc := range map[string]string{
		"replace":     "boards: []",
		"settings":    "settings: {theme_id: 1}",
		"connections": "connections: [{id: k, type: kimai, url: 'https://k.lan'}]",
	} {
		mode := porting.Merge
		if name == "replace" {
			mode = porting.Replace
		}
		if _, err := porting.ImportSpace(d, editor, space.ID, doc, mode); !errors.Is(err, access.ErrDenied) {
			t.Errorf("%s: %v, want denied", name, err)
		}
	}
	if _, err := porting.ImportSpace(d, editor, space.ID, "widgets: [{id: n, type: note, config: {text: hi}}]", porting.Merge); err != nil {
		t.Errorf("merge tiles: %v", err)
	}

	// Shared location data stays out of team spaces on import.
	owner := adminWho
	report, err := porting.ImportSpace(d, owner, space.ID, "connections: [{id: loc, type: dawarich, url: 'https://d.lan'}]", porting.Merge)
	if err != nil || report.Connections != 0 {
		t.Errorf("shared dawarich: %+v %v", report, err)
	}
}
