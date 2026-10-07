package boards_test

import (
	"database/sql"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/widgetlib"
	"andon/internal/testkit"
)

// tilesOf lists the widget ids a board shows, section by section.
func tilesOf(t *testing.T, view *boards.BoardView) []int64 {
	t.Helper()
	var out []int64
	for _, s := range view.Sections {
		for _, tile := range s.Tiles {
			out = append(out, tile.WidgetID)
		}
	}
	return out
}

// TestPutOnBoard: a library tile lands at the end of the board's first
// section; a board without sections gets one. Who may not edit the board
// is refused.
func TestPutOnBoard(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	board, err := boards.Create(d, who, space, "Übersicht")
	if err != nil {
		t.Fatal(err)
	}
	clock, err := widgetlib.Create(d, who, space, "clock", "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := boards.PutOn(d, who, board, clock); err != nil {
		t.Fatalf("put: %v", err)
	}
	view, _ := boards.View(d, who, board, boards.LayoutBoard)
	if got := tilesOf(t, view); len(got) != 1 || got[0] != clock {
		t.Fatalf("tiles: %v", got)
	}

	// No section left: one is added.
	if err := boards.DeleteSection(d, who, view.Sections[0].ID, view.Version); err != nil {
		t.Fatal(err)
	}
	if err := boards.PutOn(d, who, board, clock); err != nil {
		t.Fatalf("put without section: %v", err)
	}
	view, _ = boards.View(d, who, board, boards.LayoutBoard)
	if got := tilesOf(t, view); len(view.Sections) != 1 || len(got) != 1 {
		t.Fatalf("sections %d, tiles %v", len(view.Sections), got)
	}

	stranger, _ := testkit.User(t, d, "x@b.c", enums.RoleUser)
	if err := boards.PutOn(d, stranger, board, clock); err == nil {
		t.Fatal("stranger put a tile on a foreign board")
	}
}

// TestAddNewOnBoard: a template becomes a tile of the board's space on
// the board, with the connection it reads.
func TestAddNewOnBoard(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	scrutiny := testkit.Conn(t, d, who, space, enums.ServiceScrutiny, "https://scrutiny.test")
	board, _ := boards.Create(d, who, space, "Übersicht")

	id, err := boards.AddNew(d, who, board, "disks", &scrutiny)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	w, _ := content.Widget(d, id)
	if w == nil || w.SpaceID != space || w.Type != "disks" || w.ConnectionID == nil || *w.ConnectionID != scrutiny {
		t.Fatalf("widget: %+v", w)
	}
	view, _ := boards.View(d, who, board, boards.LayoutBoard)
	if got := tilesOf(t, view); len(got) != 1 || got[0] != id {
		t.Fatalf("tiles: %v", got)
	}
	if _, err := boards.AddNew(d, who, board, "disks", nil); err == nil {
		t.Fatal("disks without a connection")
	}
}

// TestEmptyTips: an empty board suggests a starter for each connection of
// its space that has no tile yet; only to who may edit it, and only
// while it is empty.
func TestEmptyTips(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	kimai := testkit.Conn(t, d, who, space, enums.ServiceKimai, "https://kimai.test")
	scrutiny := testkit.Conn(t, d, who, space, enums.ServiceScrutiny, "https://scrutiny.test")
	if _, err := widgetlib.Create(d, who, space, "kimai_week", "", nil, &kimai, nil); err != nil {
		t.Fatal(err)
	}
	board, _ := boards.Create(d, who, space, "Leer")

	tips, err := boards.EmptyTips(d, who, board)
	if err != nil {
		t.Fatal(err)
	}
	if len(tips) != 1 || tips[0].Type != "disks" || tips[0].ConnID == nil || *tips[0].ConnID != scrutiny || tips[0].ConnName == "" {
		t.Fatalf("tips: %+v", tips)
	}

	stranger, _ := testkit.User(t, d, "x@b.c", enums.RoleUser)
	if tips, _ := boards.EmptyTips(d, stranger, board); len(tips) != 0 {
		t.Fatalf("tips for a stranger: %+v", tips)
	}

	// A board with tiles needs none.
	if _, err := boards.AddNew(d, who, board, "clock", nil); err != nil {
		t.Fatal(err)
	}
	if tips, _ := boards.EmptyTips(d, who, board); len(tips) != 0 {
		t.Fatalf("tips on a board with tiles: %+v", tips)
	}
}

// TestFitting: a connection's templates (widgets.ForService) without the
// ones already set up for it, own types on the connection, partner
// types without one.
func TestFitting(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	borg := testkit.Conn(t, d, who, space, enums.ServiceBorgBackup, "https://borg.test")

	tips, err := boards.Fitting(d, who, borg, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(tips) == 0 || tips[0].Type != "backups" || tips[0].ConnID != nil {
		t.Fatalf("borg: %+v", tips)
	}
	if _, err := boards.AddNew(d, who, mustBoard(t, d, who, space), "backups", nil); err != nil {
		t.Fatal(err)
	}
	if tips, _ := boards.Fitting(d, who, borg, 3); len(tips) > 0 && tips[0].Type == "backups" {
		t.Fatalf("backups offered twice: %+v", tips)
	}

	kimai := testkit.Conn(t, d, who, space, enums.ServiceKimai, "https://kimai.test")
	tips, _ = boards.Fitting(d, who, kimai, 2)
	if len(tips) != 2 || tips[0].Type != "kimai_week" || tips[0].ConnID == nil || *tips[0].ConnID != kimai {
		t.Fatalf("kimai: %+v", tips)
	}

	stranger, _ := testkit.User(t, d, "x@b.c", enums.RoleUser)
	if _, err := boards.Fitting(d, stranger, kimai, 3); err == nil {
		t.Fatal("stranger reads a foreign connection's templates")
	}
}

func mustBoard(t *testing.T, d *sql.DB, who *access.Principal, space int64) int64 {
	t.Helper()
	id, err := boards.Create(d, who, space, "Board")
	if err != nil {
		t.Fatal(err)
	}
	return id
}
