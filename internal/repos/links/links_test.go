package links_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/repos/links"
	"andon/internal/testkit"
)

// verbund puts Dawarich, Kimai and Invoice Ninja into one Verbund.
func verbund(t *testing.T) (*sql.DB, int64, map[enums.ServiceType]int64) {
	t.Helper()
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	conns := map[enums.ServiceType]int64{}
	for _, s := range []enums.ServiceType{enums.ServiceDawarich, enums.ServiceKimai, enums.ServiceInvoiceNinja} {
		conns[s] = testkit.Conn(t, d, who, space, s, "https://"+string(s)+".example")
	}
	id, err := links.Add(d, "Studio", &who.UserID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []enums.ServiceType{enums.ServiceDawarich, enums.ServiceKimai, enums.ServiceInvoiceNinja} {
		if err := links.AddMember(d, id, conns[s], string(s)); err != nil {
			t.Fatal(err)
		}
	}
	return d, id, conns
}

func dropConn(t *testing.T, d *sql.DB, id int64) {
	t.Helper()
	if _, err := d.Exec("DELETE FROM connections WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
}

func TestOneMemberPerService(t *testing.T) {
	d, id, conns := verbund(t)
	if err := links.AddMember(d, id, conns[enums.ServiceKimai], "kimai"); err == nil {
		t.Fatal("same connection twice")
	}
	who, space := testkit.User(t, d, "b@b.c", enums.RoleUser)
	second := testkit.Conn(t, d, who, space, enums.ServiceKimai, "https://k2.example")
	if err := links.AddMember(d, id, second, "kimai"); !errors.Is(err, links.ErrServiceTaken) {
		t.Fatalf("second kimai: %v", err)
	}
	l, err := links.Get(d, id)
	if err != nil || l == nil || len(l.Members) != 3 {
		t.Fatalf("link: %+v %v", l, err)
	}
}

// A deleted connection leaves the Verbund; below two members it goes.
func TestDeletedConnectionShrinksVerbund(t *testing.T) {
	d, id, conns := verbund(t)
	dropConn(t, d, conns[enums.ServiceDawarich])
	l, err := links.Get(d, id)
	if err != nil || l == nil || len(l.Members) != 2 {
		t.Fatalf("after one: %+v %v", l, err)
	}
	dropConn(t, d, conns[enums.ServiceKimai])
	if l, err := links.Get(d, id); err != nil || l != nil {
		t.Fatalf("one member left, still there: %+v %v", l, err)
	}
}

// Entries: one key per member; an id sits in one entry only; an entry
// below two keys goes.
func TestEntries(t *testing.T) {
	d, id, conns := verbund(t)
	kimai, ninja := conns[enums.ServiceKimai], conns[enums.ServiceInvoiceNinja]
	now := time.Now()

	acme, err := links.PutEntry(d, id, "customers", []links.Key{{ConnID: kimai, Key: "12", State: links.KeyConfirmed}, {ConnID: ninja, Key: "Kx9", State: links.KeySuggested}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := links.PutEntry(d, id, "customers", []links.Key{{ConnID: kimai, Key: "15", State: links.KeyConfirmed}, {ConnID: ninja, State: links.KeyNone}}, now); err != nil {
		t.Fatalf("no counterpart: %v", err)
	}
	if _, err := links.PutEntry(d, id, "customers", []links.Key{{ConnID: kimai, Key: "12", State: links.KeyConfirmed}, {ConnID: ninja, Key: "Zz1", State: links.KeyConfirmed}}, now); !errors.Is(err, links.ErrKeyTaken) {
		t.Fatalf("kimai 12 twice: %v", err)
	}
	// The same id in another domain is fine.
	if _, err := links.PutEntry(d, id, "places", []links.Key{{ConnID: kimai, Key: "12", State: links.KeyConfirmed}, {ConnID: ninja, Key: "12", State: links.KeyConfirmed}}, now); err != nil {
		t.Fatalf("other domain: %v", err)
	}

	if err := links.SetKey(d, acme, id, "customers", links.Key{ConnID: ninja, Key: "Kx9", State: links.KeyConfirmed}); err != nil {
		t.Fatal(err)
	}
	list, err := links.Entries(d, id, "customers")
	if err != nil || len(list) != 2 || len(list[0].Keys) != 2 || list[0].Keys[1].State != links.KeyConfirmed {
		t.Fatalf("entries: %+v %v", list, err)
	}

	// Ninja leaves: every customer entry is down to one key and goes.
	if err := links.RemoveMember(d, id, ninja); err != nil {
		t.Fatal(err)
	}
	if list, _ := links.Entries(d, id, "customers"); len(list) != 0 {
		t.Fatalf("entries left: %+v", list)
	}
}
