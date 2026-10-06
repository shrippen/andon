package sources_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/sources"
)

// Hansei's pushed state becomes its dataset; ids it claims are kept in
// order, other values are ignored.
func TestHanseiFromPushedState(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	sctx := sources.Ctx{URL: "https://hansei.local", StateAt: at, State: map[string]any{
		"review": 2.0, "feedback": 1.0, "done": 7.0, "conformity": 0.82,
		"claimed": []any{"docs.missing:regis/kometa", 3.0, "docs.orphan:IT/Dienste/Regis/Paperless.md"},
	}}

	out, err := sources.HanseiData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.HanseiDataset)
	if d.Review != 2 || d.Feedback != 1 || d.Done != 7 || d.Conformity != 0.82 || !d.Updated.Equal(at) {
		t.Fatalf("data: %+v", d)
	}
	if len(d.Claimed) != 2 || d.Claimed[1] != "docs.orphan:IT/Dienste/Regis/Paperless.md" {
		t.Fatalf("claimed: %v", d.Claimed)
	}

	empty, err := sources.HanseiData.Fetch(context.Background(), sources.Ctx{URL: "https://hansei.local"})
	if err != nil || !empty.(*sources.HanseiDataset).Updated.IsZero() {
		t.Fatalf("empty: %+v %v", empty, err)
	}
}
