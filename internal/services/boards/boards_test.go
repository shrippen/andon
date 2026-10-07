package boards_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/boards"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func addUser(t *testing.T, q db.Queryer, email string, role enums.InstanceRole) *model.User {
	t.Helper()
	u := &model.User{Email: email, Name: email, Role: role, IsActive: true,
		Locale: enums.LocaleDE, ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(q, u); err != nil {
		t.Fatalf("add user: %v", err)
	}
	personal := &model.Space{Kind: enums.SpacePersonal, Name: u.Name, OwnerUserID: &u.ID, Version: 1}
	if err := content.AddSpace(q, personal); err != nil {
		t.Fatalf("add personal space: %v", err)
	}
	return u
}

func addWidget(t *testing.T, q db.Queryer, spaceID int64, key string) *model.Widget {
	t.Helper()
	w := &model.Widget{SpaceID: spaceID, Key: key, Type: "note", Title: "Note", Version: 1, UpdatedAt: time.Now().UTC()}
	if err := content.AddWidget(q, w); err != nil {
		t.Fatalf("add widget: %v", err)
	}
	return w
}

func TestCreateAndViewBoard(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := boards.Create(d, who, space.ID, "My Board")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	view, err := boards.View(d, who, id, boards.LayoutOverlay)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Name != "My Board" || !view.CanEdit || len(view.Sections) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestPlaceAndUnplaceWidget(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w := addWidget(t, d, space.ID, "note1")

	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	sectionID := view.Sections[0].ID

	placementID, err := boards.Place(d, who, sectionID, w.ID, view.Version)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if len(view.Sections[0].Tiles) != 1 || view.Sections[0].Tiles[0].WidgetID != w.ID {
		t.Fatalf("expected 1 tile, got %+v", view.Sections[0].Tiles)
	}

	if err := boards.Unplace(d, who, placementID, view.Version); err != nil {
		t.Fatalf("unplace: %v", err)
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if len(view.Sections[0].Tiles) != 0 {
		t.Fatalf("expected 0 tiles after unplace, got %+v", view.Sections[0].Tiles)
	}
}

func TestVersionConflict(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")

	err := boards.Rename(d, who, boardID, 999, "New Name", nil, nil, enums.LayoutGrid)
	if !errors.Is(err, boards.ErrConflict) {
		t.Fatalf("expected conflict for stale version, got %v", err)
	}
}

func TestOtherUserCannotEditPersonalBoard(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de", enums.RoleUser)
	stranger := addUser(t, d, "stranger@x.de", enums.RoleUser)
	ownerWho, _ := access.Load(d, owner.ID)
	strangerWho, _ := access.Load(d, stranger.ID)
	space, _ := content.PersonalSpace(d, owner.ID)

	boardID, err := boards.Create(d, ownerWho, space.ID, "Private")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := boards.View(d, strangerWho, boardID, boards.LayoutOverlay); err == nil {
		t.Fatal("expected stranger to be denied viewing a personal board")
	}
}

func TestArrangeByEditorReordersBoard(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w1 := addWidget(t, d, space.ID, "w1")
	w2 := addWidget(t, d, space.ID, "w2")

	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	sectionID := view.Sections[0].ID
	p1, _ := boards.Place(d, who, sectionID, w1.ID, view.Version)
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	p2, _ := boards.Place(d, who, sectionID, w2.ID, view.Version)
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)

	target, _, err := boards.Arrange(d, who, boardID, view.Version, map[int64][]int64{sectionID: {p2, p1}}, boards.LayoutBoard)
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if target != boards.LayoutBoard {
		t.Fatalf("expected editor arrange to target the board, got %v", target)
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if view.Sections[0].Tiles[0].WidgetID != w2.ID {
		t.Fatalf("expected w2 first after reorder, got %+v", view.Sections[0].Tiles)
	}
}

// TestArrangeInOwnLayoutKeepsBoard: an editor dragging in "Mein Layout"
// changes only their own layout, not the board everybody sees.
func TestArrangeInOwnLayoutKeepsBoard(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w1 := addWidget(t, d, space.ID, "w1")
	w2 := addWidget(t, d, space.ID, "w2")

	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	sectionID := view.Sections[0].ID
	p1, _ := boards.Place(d, who, sectionID, w1.ID, view.Version)
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	p2, _ := boards.Place(d, who, sectionID, w2.ID, view.Version)
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)

	target, version, err := boards.Arrange(d, who, boardID, view.Version, map[int64][]int64{sectionID: {p2, p1}}, boards.LayoutOverlay)
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if target != boards.LayoutOverlay || version != view.Version {
		t.Fatalf("own layout drag reached the board: %v, version %d → %d", target, view.Version, version)
	}
	placements, _ := content.Board(d, boardID)
	if placements.Sections[0].Placements[0].ID != p1 {
		t.Fatal("board order changed")
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if view.Sections[0].Tiles[0].WidgetID != w2.ID {
		t.Fatalf("own layout not reordered: %+v", view.Sections[0].Tiles)
	}
}

func TestFoldAndShowUseOverlay(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w := addWidget(t, d, space.ID, "w1")

	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	sectionID := view.Sections[0].ID
	placementID, _ := boards.Place(d, who, sectionID, w.ID, view.Version)

	if err := boards.FoldSection(d, who, boardID, sectionID, boards.FoldClosed); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if err := boards.ShowTile(d, who, boardID, placementID, boards.VisHidden); err != nil {
		t.Fatalf("hide: %v", err)
	}

	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if !view.Sections[0].Collapsed {
		t.Fatal("expected section collapsed via overlay")
	}
	if !view.Sections[0].Tiles[0].Hidden {
		t.Fatal("expected tile hidden via overlay")
	}
	if !view.HasOverlay {
		t.Fatal("expected HasOverlay true")
	}

	if err := boards.ResetOverlay(d, who, boardID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if view.Sections[0].Collapsed || view.HasOverlay {
		t.Fatal("expected overlay cleared")
	}
}

func TestHistoryAndRestore(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w := addWidget(t, d, space.ID, "w1")

	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	sectionID := view.Sections[0].ID
	boards.Place(d, who, sectionID, w.ID, view.Version)

	history, err := boards.History(d, who, boardID)
	if err != nil || len(history) < 2 {
		t.Fatalf("expected at least 2 revisions (create + place), got %d err=%v", len(history), err)
	}

	// Restore the first revision (empty section, no widgets).
	oldest := history[len(history)-1]
	if err := boards.Restore(d, who, boardID, oldest.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if len(view.Sections[0].Tiles) != 0 {
		t.Fatalf("expected restored board to have no tiles, got %+v", view.Sections[0].Tiles)
	}
}

// TestTileRows: editors make a tile tall for everybody (kept in history),
// every viewer may override it in the own layout.
func TestTileRows(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w := addWidget(t, d, space.ID, "w1")
	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	placementID, _ := boards.Place(d, who, view.Sections[0].ID, w.ID, view.Version)

	tile := func() boards.Tile {
		v, err := boards.View(d, who, boardID, boards.LayoutOverlay)
		if err != nil {
			t.Fatal(err)
		}
		return v.Sections[0].Tiles[0]
	}
	if tile().Rows != 1 {
		t.Fatalf("new tile rows %d, want 1", tile().Rows)
	}

	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if err := boards.SetTileRows(d, who, placementID, 5, view.Version); err != nil {
		t.Fatalf("set rows: %v", err)
	}
	if tile().Rows != boards.MaxTileRows {
		t.Fatalf("rows %d, want the cap %d", tile().Rows, boards.MaxTileRows)
	}

	if err := boards.SetMyTileRows(d, who, boardID, placementID, 1); err != nil {
		t.Fatalf("my rows: %v", err)
	}
	if tile().Rows != 1 {
		t.Fatalf("overlay not applied: %d", tile().Rows)
	}
	boards.ResetOverlay(d, who, boardID)

	// The tall height survives a restore of the latest revision.
	history, _ := boards.History(d, who, boardID)
	if err := boards.Restore(d, who, boardID, history[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if tile().Rows != boards.MaxTileRows {
		t.Fatalf("restored rows %d", tile().Rows)
	}
}

// TestTileCols: like heights, editors make a tile wide for everybody and
// every viewer may override it; restore keeps the width.
func TestTileCols(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")
	w := addWidget(t, d, space.ID, "w1")
	view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
	placementID, _ := boards.Place(d, who, view.Sections[0].ID, w.ID, view.Version)

	tile := func() boards.Tile {
		v, err := boards.View(d, who, boardID, boards.LayoutOverlay)
		if err != nil {
			t.Fatal(err)
		}
		return v.Sections[0].Tiles[0]
	}
	if tile().Cols != 1 {
		t.Fatalf("new tile cols %d, want 1", tile().Cols)
	}

	view, _ = boards.View(d, who, boardID, boards.LayoutOverlay)
	if err := boards.SetTileCols(d, who, placementID, 5, view.Version); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if tile().Cols != boards.MaxTileCols {
		t.Fatalf("cols %d, want the cap %d", tile().Cols, boards.MaxTileCols)
	}

	if err := boards.SetMyTileCols(d, who, boardID, placementID, 1); err != nil {
		t.Fatalf("my cols: %v", err)
	}
	if tile().Cols != 1 {
		t.Fatalf("overlay not applied: %d", tile().Cols)
	}
	boards.ResetOverlay(d, who, boardID)

	history, _ := boards.History(d, who, boardID)
	if err := boards.Restore(d, who, boardID, history[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if tile().Cols != boards.MaxTileCols {
		t.Fatalf("restored cols %d", tile().Cols)
	}
}

// TestBoardLayout: masonry is stored; unknown layouts fall back to the grid.
func TestBoardLayout(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	boardID, _ := boards.Create(d, who, space.ID, "B")

	for _, c := range []struct{ in, want enums.BoardLayout }{{enums.LayoutMasonry, enums.LayoutMasonry}, {"bogus", enums.LayoutGrid}} {
		view, _ := boards.View(d, who, boardID, boards.LayoutOverlay)
		if err := boards.Rename(d, who, boardID, view.Version, "B", nil, nil, c.in); err != nil {
			t.Fatal(err)
		}
		if view, _ = boards.View(d, who, boardID, boards.LayoutOverlay); view.Layout != c.want {
			t.Fatalf("%q: %q", c.in, view.Layout)
		}
	}
}

// TestBoardNameBounded: a pasted essay is no board name; a 300-character
// name made every page 4400 px wide.
func TestBoardNameBounded(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := boards.Create(d, who, space.ID, strings.Repeat("X", 300))
	if err != nil {
		t.Fatal(err)
	}
	view, _ := boards.View(d, who, id, boards.LayoutOverlay)
	if n := len([]rune(view.Name)); n > boards.MaxNameLen {
		t.Fatalf("name of %d runes", n)
	}
	if err := boards.Rename(d, who, id, view.Version, strings.Repeat("Ü", 300), nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view, _ = boards.View(d, who, id, boards.LayoutOverlay)
	if n := len([]rune(view.Name)); n > boards.MaxNameLen {
		t.Fatalf("renamed to %d runes", n)
	}
}

// TestUndoStepsBack: undo goes back one change at a time; it used to
// swap the two newest versions, so a second undo redid the first.
func TestUndoStepsBack(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	id, _ := boards.Create(d, who, space.ID, "B")

	for _, title := range []string{"Zwei", "Drei"} {
		view, _ := boards.View(d, who, id, boards.LayoutOverlay)
		if _, err := boards.AddSection(d, who, id, view.Version, title); err != nil {
			t.Fatal(err)
		}
	}
	for want := 2; want >= 1; want-- {
		if err := boards.Undo(d, who, id); err != nil {
			t.Fatalf("undo to %d sections: %v", want, err)
		}
		if view, _ := boards.View(d, who, id, boards.LayoutOverlay); len(view.Sections) != want {
			t.Fatalf("after undo: %d sections, want %d", len(view.Sections), want)
		}
	}
	if err := boards.Undo(d, who, id); !errors.Is(err, boards.ErrNothingToUndo) {
		t.Fatalf("undo past the first version: %v", err)
	}
}

// Deleting a tile that sits on two boards can be undone on each: undo
// brings the tile back (once, the second board takes the same one).
func TestDeleteWidgetUndo(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	w := addWidget(t, d, space.ID, "note1")
	var ids []int64
	for _, name := range []string{"A", "B"} {
		id, _ := boards.Create(d, who, space.ID, name)
		view, _ := boards.View(d, who, id, boards.LayoutOverlay)
		if _, err := boards.Place(d, who, view.Sections[0].ID, w.ID, view.Version); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	n, err := boards.DeleteWidget(d, who, w.ID)
	if err != nil || n != 2 {
		t.Fatalf("delete: %d boards, %v", n, err)
	}
	var back []int64
	for _, id := range ids {
		if err := boards.Undo(d, who, id); err != nil {
			t.Fatalf("undo: %v", err)
		}
		view, _ := boards.View(d, who, id, boards.LayoutOverlay)
		if len(view.Sections[0].Tiles) != 1 {
			t.Fatalf("board %d: tile not back: %+v", id, view.Sections[0].Tiles)
		}
		back = append(back, view.Sections[0].Tiles[0].WidgetID)
	}
	if back[0] != back[1] {
		t.Fatalf("two copies: %v", back)
	}
}
