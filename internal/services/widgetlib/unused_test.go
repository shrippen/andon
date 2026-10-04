package widgetlib_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/widgetlib"
)

// DeleteUnused removes only tiles that sit on no board and that the
// caller may manage; it skips the rest without failing.
func TestDeleteUnused(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	other := addUser(t, d, "x@b.c")
	whoOther, _ := access.Load(d, other.ID)
	otherSpace, _ := content.PersonalSpace(d, other.ID)

	unused, _ := widgetlib.Create(d, who, space.ID, "note", "Unused", nil, nil, nil)
	placed, _ := widgetlib.Create(d, who, space.ID, "note", "Placed", nil, nil, nil)
	foreign, _ := widgetlib.Create(d, whoOther, otherSpace.ID, "note", "Foreign", nil, nil, nil)

	board := &model.Board{SpaceID: space.ID, Slug: "start", Name: "Start", Version: 1}
	if err := content.AddBoard(d, board); err != nil {
		t.Fatalf("add board: %v", err)
	}
	sec := &model.Section{BoardID: board.ID, Title: "Oben", Size: enums.TileMedium, Sort: enums.SortManual, Area: "main"}
	if err := content.AddSection(d, sec); err != nil {
		t.Fatalf("add section: %v", err)
	}
	if err := content.AddPlacement(d, &model.Placement{SectionID: sec.ID, WidgetID: placed}); err != nil {
		t.Fatalf("add placement: %v", err)
	}

	n, err := widgetlib.DeleteUnused(d, who, []int64{unused, placed, foreign, 9999})
	if err != nil || n != 1 {
		t.Fatalf("expected 1 deleted, got %d, %v", n, err)
	}
	for id, want := range map[int64]bool{unused: false, placed: true, foreign: true} {
		w, _ := content.Widget(d, id)
		if (w != nil) != want {
			t.Errorf("widget %d: exists=%v, want %v", id, w != nil, want)
		}
	}
}

// The library tells which tiles the caller may delete.
func TestLibraryCanDelete(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	if _, err := widgetlib.Create(d, who, space.ID, "note", "Mine", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	lib, err := widgetlib.Library(d, who)
	if err != nil || len(lib) != 1 || !lib[0].CanDelete {
		t.Fatalf("expected one deletable tile, got %+v, %v", lib, err)
	}
}
