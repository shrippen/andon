package boards_test

import (
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/themes"
)

// TestBoardThemeMustBeUsable: a board may only wear a theme its editor
// may use, not someone else's private one.
func TestBoardThemeMustBeUsable(t *testing.T) {
	d := openTestDB(t)
	builtin, _ := themes.EnsureBuiltin(d)
	owner := addUser(t, d, "o@x.de", enums.RoleUser)
	other := addUser(t, d, "x@x.de", enums.RoleUser)
	ownerWho, _ := access.Load(d, owner.ID)
	otherWho, _ := access.Load(d, other.ID)
	otherSpace, _ := content.PersonalSpace(d, other.ID)
	private, err := themes.Duplicate(d, otherWho, builtin, otherSpace.ID, "Mine")
	if err != nil {
		t.Fatal(err)
	}

	space, _ := content.PersonalSpace(d, owner.ID)
	board := &model.Board{SpaceID: space.ID, Slug: "b", Name: "B", Version: 1}
	if err := content.AddBoard(d, board); err != nil {
		t.Fatal(err)
	}
	if err := boards.Rename(d, ownerWho, board.ID, 1, "B", &private, nil, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("foreign theme: %v", err)
	}
	if err := boards.Rename(d, ownerWho, board.ID, 1, "B", &builtin, nil, ""); err != nil {
		t.Fatalf("built-in theme: %v", err)
	}
}
