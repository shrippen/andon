package content_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/misc"
)

// TestBoardGoneLeavesNothing: a deleted board takes its revisions and
// shares along. SQLite hands its id to the next board, which must not
// inherit them (a "restore" overwrote another board with them).
func TestBoardGoneLeavesNothing(t *testing.T) {
	q := openTestDB(t)
	sp := addSpace(t, q)
	b := &model.Board{SpaceID: sp.ID, Slug: "old", Name: "Old", Version: 1}
	content.AddBoard(q, b)
	content.AddRevision(q, &model.Revision{Kind: enums.RevisionBoard, EntityID: b.ID, SpaceID: sp.ID, Version: 1, Data: map[string]any{}})
	if err := misc.AddShare(q, &model.Share{ResourceKind: enums.ResourceBoard, ResourceID: b.ID, GranteeKind: enums.GranteeTeam, GranteeID: 1, Right: enums.RightView}); err != nil {
		t.Fatal(err)
	}

	if err := content.RemoveBoard(q, b.ID); err != nil {
		t.Fatal(err)
	}
	next := &model.Board{SpaceID: sp.ID, Slug: "new", Name: "New", Version: 1}
	content.AddBoard(q, next)
	if next.ID != b.ID {
		t.Logf("id not reused (%d, %d); still checking the old id", b.ID, next.ID)
	}
	revs, _ := content.Revisions(q, enums.RevisionBoard, b.ID)
	shares, _ := misc.SharesFor(q, enums.ResourceBoard, b.ID)
	if len(revs) != 0 || len(shares) != 0 {
		t.Fatalf("left behind: %d revisions, %d shares", len(revs), len(shares))
	}
}
