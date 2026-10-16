package hints_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/users"
	"andon/internal/rules"
	"andon/internal/services/hints"
)

// TestResolveOtherOwners: a template that became a fixed connection (or
// lost a holder) leaves the old owner's hints open; nothing would ever
// resolve them, and each case showed twice.
func TestResolveOtherOwners(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)
	conn := addConnection(t, q, sid, "snipe")
	u := &model.User{Email: "a@b.c", Name: "A", Role: enums.RoleUser, IsActive: true,
		Locale: enums.LocaleDE, ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(q, u); err != nil {
		t.Fatal(err)
	}
	ids := []string{"kimai.missing_day"}
	f := []rules.Finding{finding("warranty:834")}

	// Once a personal login, now the space's.
	if _, err := hints.Sync(q, sid, &u.ID, &conn, ids, f); err != nil {
		t.Fatal(err)
	}
	if _, err := hints.Sync(q, sid, nil, &conn, ids, f); err != nil {
		t.Fatal(err)
	}
	if open, _ := dataHintsOfScope(q, sid); len(open) != 2 {
		t.Fatalf("setup: %d open", len(open))
	}

	n, err := hints.ResolveOthers(q, sid, conn, []*int64{nil})
	if err != nil || n != 1 {
		t.Fatalf("resolved %d, err %v", n, err)
	}
	if open, _ := dataHintsOfScope(q, sid); len(open) != 1 {
		t.Fatalf("after: %d open", len(open))
	}

	// The remaining owner's hints stay.
	if n, err := hints.ResolveOthers(q, sid, conn, []*int64{nil}); err != nil || n != 0 {
		t.Fatalf("second pass resolved %d, err %v", n, err)
	}
}
