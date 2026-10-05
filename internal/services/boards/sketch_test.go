package boards_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/boards"
)

// TestSketchesAndOrder: a board's sketch has one part per section with a
// tile per placement; a new order puts the named boards first and keeps
// the rest after them.
func TestSketchesAndOrder(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	a, _ := boards.Create(d, who, space.ID, "A")
	b, _ := boards.Create(d, who, space.ID, "B")
	c, _ := boards.Create(d, who, space.ID, "C")

	view, _ := boards.View(d, who, a, boards.LayoutOverlay)
	if _, err := boards.Place(d, who, view.Sections[0].ID, addWidget(t, d, space.ID, "n1").ID, view.Version); err != nil {
		t.Fatal(err)
	}

	listed, _ := boards.Listed(d, who)
	sketches, err := boards.Sketches(d, listed)
	if err != nil {
		t.Fatal(err)
	}
	if s := sketches[a]; s.Tiles != 1 || len(s.Sections) != 1 || s.Sections[0].Cols < 1 || s.Sections[0].Tiles[0].Cols != 1 {
		t.Fatalf("sketch of A: %+v", s)
	}
	if s := sketches[b]; s.Tiles != 0 {
		t.Fatalf("sketch of B: %+v", s)
	}

	if err := boards.SetNavOrder(d, who, []int64{c, a, c, 999}); err != nil {
		t.Fatal(err)
	}
	listed, _ = boards.Listed(d, who)
	if got := ids(listed); len(got) != 3 || got[0] != c || got[1] != a || got[2] != b {
		t.Fatalf("order: %v", got)
	}
}
