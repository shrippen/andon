package closeticks_test

import (
	"errors"
	"slices"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/closeticks"
	"andon/internal/testkit"
)

// A tick is kept per month and a second click takes it back; other tiles
// cannot tick.
func TestToggle(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	tile := testkit.Place(t, d, who, space, closeticks.WidgetType, nil, nil)

	if err := closeticks.Toggle(d, who, tile, "2026-08", "vat"); err != nil {
		t.Fatal(err)
	}
	ticks, err := closeticks.Of(d, who)
	if err != nil || !slices.Equal(ticks["2026-08"], []string{"vat"}) {
		t.Fatalf("ticks %v, %v", ticks, err)
	}
	if err := closeticks.Toggle(d, who, tile, "2026-08", "vat"); err != nil {
		t.Fatal(err)
	}
	if ticks, _ := closeticks.Of(d, who); len(ticks["2026-08"]) != 0 {
		t.Fatalf("untick: %v", ticks)
	}
	note := testkit.Place(t, d, who, space, "note", nil, nil)
	if err := closeticks.Toggle(d, who, note, "2026-08", "vat"); !errors.Is(err, closeticks.ErrNotClose) {
		t.Fatalf("note tile: %v", err)
	}
}
