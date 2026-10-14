package journal_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/journal"
	"andon/internal/testkit"
)

// The journal lists the caller's writes to services only, per area;
// links and archivings carry what their undo needs, others none; nobody
// undoes another's entry.
func TestEntries(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "a@b.c", enums.RoleUser)
	other, _ := testkit.User(t, d, "x@b.c", enums.RoleUser)
	log := func(uid int64, action string, detail map[string]any) {
		if err := auditsvc.Log(d, &uid, action, "T", "", detail); err != nil {
			t.Fatal(err)
		}
	}
	log(who.UserID, "login.password", nil)
	log(who.UserID, "vatcheck.skip", map[string]any{"txn": "b2", "scope": "c1"})
	log(who.UserID, "receipts.link", map[string]any{"docs": "201", "expense": "e1"})
	log(who.UserID, "receipts.link", map[string]any{"docs": "202"}) // before keys were kept
	log(who.UserID, "kimai.start", nil)
	log(other.UserID, "vatcheck.skip", map[string]any{"txn": "b9"})

	all, err := journal.Entries(d, who, journal.AreaAll)
	if err != nil {
		t.Fatal(err)
	}
	undo := map[string]int{}
	for _, e := range all {
		if e.Undo {
			undo[e.Action]++
		}
	}
	if len(all) != 4 || undo["vatcheck.skip"] != 1 || undo["receipts.link"] != 1 || all[0].Label() != "journal.act_kimai_start" {
		t.Fatalf("entries: %+v", all)
	}
	vat, _ := journal.Entries(d, who, journal.AreaVAT)
	if len(vat) != 1 || vat[0].Area != journal.AreaVAT {
		t.Errorf("vat area: %+v", vat)
	}

	theirs, _ := journal.Entries(d, other, journal.AreaAll)
	if err := journal.Undo(context.Background(), d, who, theirs[0].ID, ""); !errors.Is(err, journal.ErrNotFound) {
		t.Errorf("undo of another's entry: %v", err)
	}
	if err := journal.Undo(context.Background(), d, who, all[0].ID, ""); !errors.Is(err, journal.ErrNoUndo) {
		t.Errorf("undo of a timer start: %v", err)
	}
}
