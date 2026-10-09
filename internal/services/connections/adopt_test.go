package connections_test

import (
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/repos/links"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

// TestAdopt: a second connection of the same service takes over what
// hangs on the old one (tiles, Verbund members and ids, hints with their
// history, fetch history, webhook events, mail reads, shares), then the
// old one goes. A hint both carry keeps the old one's history.
func TestAdopt(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	other := addUser(t, d, "other@x.de")
	who, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)
	add := func(svc enums.ServiceType, name, url string) int64 {
		id, err := connections.Create(d, who, space.ID, svc, name, url, enums.CredentialShared, "tok", connections.TLSVerify, nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := add(enums.ServiceKimai, "Alt", "https://kimai.old")
	fresh := add(enums.ServiceKimai, "Neu", "https://kimai.new")
	ninja := add(enums.ServiceInvoiceNinja, "Ninja", "https://ninja.lan")
	oldKey := mustConn(t, d, old).Key
	day := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	err := db.WithTx(d, func(tx *sql.Tx) error {
		tile := &model.Widget{SpaceID: space.ID, Key: "k", Type: "kimai_week", Config: map[string]any{}, ConnectionID: &old, Version: 1}
		link := &model.Widget{SpaceID: space.ID, Key: "l", Type: "link", Config: map[string]any{"info": map[string]any{"connection": oldKey}}, Version: 1}
		for _, w := range []*model.Widget{tile, link} {
			if err := content.AddWidget(tx, w); err != nil {
				return err
			}
		}
		lid, err := links.Add(tx, "Firma", &owner.ID, day)
		if err != nil {
			return err
		}
		if err := links.AddMember(tx, lid, old, "kimai"); err != nil {
			return err
		}
		if err := links.AddMember(tx, lid, ninja, "invoiceninja"); err != nil {
			return err
		}
		if _, err := links.PutEntry(tx, lid, "customers", []links.Key{{ConnID: old, Key: "12", State: links.KeyConfirmed},
			{ConnID: ninja, Key: "Kx9", State: links.KeyConfirmed}}, day); err != nil {
			return err
		}
		for _, h := range []*model.Hint{
			{SpaceID: space.ID, Fingerprint: itoa(old) + ":kimai.over", Rule: "r", Message: "m", ConnectionID: &old, FirstSeen: day, LastSeen: day},
			{SpaceID: space.ID, Fingerprint: itoa(fresh) + ":kimai.over", Rule: "r", Message: "m", ConnectionID: &fresh, FirstSeen: day, LastSeen: day},
			{SpaceID: space.ID, Fingerprint: itoa(old) + ":kimai.gap", Rule: "r", Message: "m", ConnectionID: &old, FirstSeen: day, LastSeen: day},
		} {
			if err := data.AddHint(tx, h); err != nil {
				return err
			}
		}
		for _, id := range []int64{old, fresh} {
			if err := data.RecordFetch(tx, id, day, 100, ""); err != nil {
				return err
			}
		}
		if err := data.AddHookEvent(tx, &model.HookEvent{ConnectionID: old, Event: "e", Subject: "s", At: day}); err != nil {
			return err
		}
		if err := data.SaveMailRead(tx, old, 7, map[string]any{"a": 1}); err != nil {
			return err
		}
		return misc.AddShare(tx, &model.Share{ResourceKind: enums.ResourceConnection, ResourceID: old,
			GranteeKind: enums.GranteeUser, GranteeID: other.ID, Right: enums.RightUse})
	})
	if err != nil {
		t.Fatal(err)
	}
	oldHint, _ := data.HintByPrint(d, space.ID, nil, itoa(old)+":kimai.over")

	if err := connections.Adopt(d, who, fresh, old); err != nil {
		t.Fatal(err)
	}

	if c, _ := content.Connection(d, old); c != nil {
		t.Fatal("old connection kept")
	}
	widgets, _ := content.Widgets(d, []int64{space.ID})
	for _, w := range widgets {
		if w.Key == "k" && (w.ConnectionID == nil || *w.ConnectionID != fresh) {
			t.Fatalf("tile not moved: %+v", w)
		}
		if w.Key == "l" && w.Config["info"].(map[string]any)["connection"] != mustConn(t, d, fresh).Key {
			t.Fatalf("link info not moved: %+v", w.Config)
		}
	}
	all, _ := links.All(d)
	if len(all) != 1 || !hasMember(all[0], fresh) {
		t.Fatalf("Verbund: %+v", all)
	}
	entries, _ := links.Entries(d, all[0].ID, "customers")
	if len(entries) != 1 || !hasKey(entries[0], fresh, "12") {
		t.Fatalf("ids: %+v", entries)
	}
	if h, _ := data.HintByPrint(d, space.ID, nil, itoa(fresh)+":kimai.over"); h == nil || h.ID != oldHint.ID || *h.ConnectionID != fresh {
		t.Fatalf("shared hint: %+v, want the old one's (%d)", h, oldHint.ID)
	}
	if h, _ := data.HintByPrint(d, space.ID, nil, itoa(fresh)+":kimai.gap"); h == nil {
		t.Fatal("old hint not moved")
	}
	if n, _ := data.Fetches(d, fresh, day.Format(time.DateOnly)); n != 2 {
		t.Fatalf("fetch history: %d", n)
	}
	if ev, _ := data.HookEvents(d, fresh, day.Add(-time.Hour)); len(ev) != 1 {
		t.Fatalf("webhook events: %+v", ev)
	}
	if reads, _ := data.MailReads(d, fresh); len(reads) != 1 {
		t.Fatalf("mail reads: %+v", reads)
	}
	if shares, _ := misc.SharesFor(d, enums.ResourceConnection, fresh); len(shares) != 1 || shares[0].GranteeID != other.ID {
		t.Fatalf("shares: %+v", shares)
	}
}

// TestAdoptRefuses: only another connection of the same service in the
// same space; both need MANAGE.
func TestAdoptRefuses(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	who, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)
	instance := addInstance(t, d)
	setAdmin(t, d, owner)
	who, _ = access.Load(d, owner.ID)
	add := func(spaceID int64, svc enums.ServiceType) int64 {
		id, err := connections.Create(d, who, spaceID, svc, string(svc), "https://"+string(svc)+".lan", enums.CredentialShared, "", connections.TLSVerify, nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	kimai := add(space.ID, enums.ServiceKimai)
	ninja := add(space.ID, enums.ServiceInvoiceNinja)
	elsewhere := add(instance.ID, enums.ServiceKimai)

	cases := map[string]struct {
		from int64
		want error
	}{
		"itself":        {kimai, connections.ErrAdoptSelf},
		"other service": {ninja, connections.ErrAdoptService},
		"other space":   {elsewhere, connections.ErrAdoptSpace},
	}
	for name, c := range cases {
		if err := connections.Adopt(d, who, kimai, c.from); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	stranger := addUser(t, d, "s@x.de")
	strangerWho, _ := access.Load(d, stranger.ID)
	if err := connections.Adopt(d, strangerWho, kimai, elsewhere); !errors.Is(err, access.ErrDenied) && !errors.Is(err, connections.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
}

func mustConn(t *testing.T, d *sql.DB, id int64) *model.Connection {
	t.Helper()
	c, err := content.Connection(d, id)
	if err != nil || c == nil {
		t.Fatalf("connection %d: %v", id, err)
	}
	return c
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func hasMember(l links.Link, connID int64) bool {
	for _, m := range l.Members {
		if m.ConnID == connID {
			return true
		}
	}
	return false
}

func hasKey(e links.Entry, connID int64, key string) bool {
	for _, k := range e.Keys {
		if k.ConnID == connID && k.Key == key {
			return true
		}
	}
	return false
}
