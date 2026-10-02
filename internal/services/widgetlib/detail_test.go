package widgetlib

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/model"
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
	_, err := LoadDetail(context.Background(), nil, &model.Widget{Type: "clock"}, time.Now())
	if !errors.Is(err, ErrNoDetail) {
		t.Fatalf("err = %v", err)
	}
}
