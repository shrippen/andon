package boards_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/boards"
)

// TestNavOrder: moving a board changes its place in the navigation,
// hiding takes it out of the navigation but keeps it in the full list,
// and boards the order does not know yet come last.
func TestNavOrder(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c", enums.RoleUser)
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	a, _ := boards.Create(d, who, space.ID, "A")
	b, _ := boards.Create(d, who, space.ID, "B")

	if err := boards.MoveNav(d, who, b, boards.MoveUp); err != nil {
		t.Fatal(err)
	}
	c, _ := boards.Create(d, who, space.ID, "C")
	if err := boards.ToggleNav(d, who, a); err != nil {
		t.Fatal(err)
	}

	all, err := boards.Listed(d, who)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(all); len(got) != 3 || got[0] != b || got[1] != a || got[2] != c || !all[1].Hidden {
		t.Fatalf("listed: %+v", all)
	}
	nav, err := boards.Nav(d, who)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(nav); len(got) != 2 || got[0] != b || got[1] != c {
		t.Fatalf("nav: %+v", nav)
	}
}

func ids(refs []boards.BoardRef) []int64 {
	var out []int64
	for _, r := range refs {
		out = append(out, r.ID)
	}
	return out
}
