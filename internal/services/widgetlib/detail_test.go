package widgetlib

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/model"
	"andon/internal/services/hints"
)

// TestHasDetail: only registered types offer a detail dialog.
func TestHasDetail(t *testing.T) {
	if !HasDetail("link") {
		t.Error("link has no detail")
	}
	if HasDetail("clock") {
		t.Error("clock has a detail")
	}
}

// TestLoadDetailUnknownType: a type without a loader answers ErrNoDetail
// before it touches the database.
func TestLoadDetailUnknownType(t *testing.T) {
	_, err := LoadDetail(context.Background(), nil, nil, &model.Widget{Type: "clock"}, time.Now())
	if !errors.Is(err, ErrNoDetail) {
		t.Fatalf("err = %v", err)
	}
}

// TestOfConnections: a dialog lists the hints of its tile's connections
// and those tied to none, not the same hint of another connection of the
// service (two Paperless connections, one hint each).
func TestOfConnections(t *testing.T) {
	one, two := int64(1), int64(2)
	found := []hints.View{{ID: 10, ConnectionID: &one}, {ID: 11, ConnectionID: &two}, {ID: 12}}
	got := ofConnections(found, []int64{1})
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 12 {
		t.Fatalf("%+v", got)
	}
}
