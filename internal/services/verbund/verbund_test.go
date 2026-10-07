package verbund_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	"andon/internal/services/verbund"
	"andon/internal/testkit"
)

// world: an instance space with one Kimai by the admin, and a user's
// own space; principals are loaded after the instance space exists.
type world struct {
	d             *sql.DB
	admin, user   *access.Principal
	instance, own int64
}

func newWorld(t *testing.T) world {
	t.Helper()
	d := testkit.DB(t)
	instance := testkit.Instance(t, d)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, own := testkit.User(t, d, "user@x.de", enums.RoleUser)
	return world{d: d, admin: admin, user: user, instance: instance, own: own}
}

func (w world) conn(t *testing.T, space int64, s enums.ServiceType) int64 {
	t.Helper()
	who := w.user
	if space == w.instance {
		who = w.admin
	}
	return testkit.Conn(t, w.d, who, space, s, "https://"+string(s)+".example")
}

func partner(t *testing.T, w world, at verbund.Asker, s enums.ServiceType) (int64, verbund.PartnerState) {
	t.Helper()
	c, state, err := verbund.Partner(w.d, w.user, at, s)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return 0, state
	}
	return c.ID, state
}

func TestCreateRules(t *testing.T) {
	w := newWorld(t)
	daw := w.conn(t, w.own, enums.ServiceDawarich)
	kimai := w.conn(t, w.own, enums.ServiceKimai)
	kimai2 := w.conn(t, w.own, enums.ServiceKimai)
	shared := w.conn(t, w.instance, enums.ServiceInvoiceNinja)

	cases := []struct {
		name  string
		conns []int64
		want  error
	}{
		{"", []int64{daw, kimai}, verbund.ErrNoName},
		{"x", []int64{daw}, verbund.ErrTooFew},
		{"x", []int64{daw, daw}, verbund.ErrTooFew},
		{"x", []int64{kimai, kimai2}, verbund.ErrServiceTwice},
		{"x", []int64{daw, shared}, access.ErrDenied}, // USE only on the instance's Ninja
	}
	for _, c := range cases {
		if _, err := verbund.Create(w.d, w.user, c.name, c.conns, ""); !errors.Is(err, c.want) {
			t.Errorf("%q %v: %v, want %v", c.name, c.conns, err, c.want)
		}
	}

	id, err := verbund.Create(w.d, w.user, "Studio", []int64{daw, kimai}, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := verbund.Get(w.d, w.user, id)
	if err != nil || len(v.Members) != 2 || !v.CanEdit {
		t.Fatalf("view: %+v %v", v, err)
	}
	if err := verbund.AddMember(w.d, w.user, id, kimai2, ""); !errors.Is(err, verbund.ErrServiceTwice) {
		t.Fatalf("second kimai: %v", err)
	}
	if err := verbund.Rename(w.d, w.user, id, " ", ""); !errors.Is(err, verbund.ErrNoName) {
		t.Fatalf("empty name: %v", err)
	}

	// The admin sees nothing of it: the user's own space is private.
	if _, err := verbund.Get(w.d, w.admin, id); !errors.Is(err, verbund.ErrNotFound) {
		t.Fatalf("admin: %v", err)
	}

	// Down to one member, the Verbund goes.
	if err := verbund.RemoveMember(w.d, w.user, id, daw, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := verbund.Get(w.d, w.user, id); !errors.Is(err, verbund.ErrNotFound) {
		t.Fatalf("after last but one left: %v", err)
	}
}

// A Verbund with a member the caller may only use is visible but not
// editable.
func TestUseOnlyMemberIsReadOnly(t *testing.T) {
	w := newWorld(t)
	sure := testkit.Conn(t, w.d, w.admin, w.instance, enums.ServiceSure, "https://s.example")
	ninja := testkit.Conn(t, w.d, w.admin, w.instance, enums.ServiceInvoiceNinja, "https://n.example")
	id, err := verbund.Create(w.d, w.admin, "Firma", []int64{sure, ninja}, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := verbund.Get(w.d, w.user, id)
	if err != nil || v.CanEdit {
		t.Fatalf("user view: %+v %v", v, err)
	}
	if err := verbund.Rename(w.d, w.user, id, "Mine", ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("rename: %v", err)
	}
	list, err := verbund.List(w.d, w.user, w.instance)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestPartner(t *testing.T) {
	w := newWorld(t)
	daw1 := w.conn(t, w.own, enums.ServiceDawarich)
	daw2 := w.conn(t, w.own, enums.ServiceDawarich)
	kimai1 := w.conn(t, w.own, enums.ServiceKimai)

	// One Kimai in the space: the implicit partner of everyone.
	if id, state := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw1}, enums.ServiceKimai); id != kimai1 || state != verbund.PartnerFound {
		t.Fatalf("single: %d %s", id, state)
	}
	if _, state := partner(t, w, verbund.Asker{SpaceID: w.own}, enums.ServiceSure); state != verbund.PartnerNone {
		t.Fatalf("no sure: %s", state)
	}

	// Two Kimai: ambiguous until a Verbund says which.
	kimai2 := w.conn(t, w.own, enums.ServiceKimai)
	if _, state := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw1}, enums.ServiceKimai); state != verbund.PartnerAmbiguous {
		t.Fatalf("two kimai: %s", state)
	}
	first, err := verbund.Create(w.d, w.user, "Eigene Firma", []int64{daw1, kimai1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw1}, enums.ServiceKimai); id != kimai1 {
		t.Fatalf("linked: %d", id)
	}
	// Kimai 1 belongs to Dawarich 1: Dawarich 2 gets the free Kimai 2.
	if id, state := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw2}, enums.ServiceKimai); id != kimai2 || state != verbund.PartnerFound {
		t.Fatalf("free one: %d %s", id, state)
	}
	// A tile without connection still has to choose ...
	if _, state := partner(t, w, verbund.Asker{SpaceID: w.own}, enums.ServiceKimai); state != verbund.PartnerAmbiguous {
		t.Fatalf("tile: %s", state)
	}
	// ... and does so by its Verbund.
	if id, _ := partner(t, w, verbund.Asker{SpaceID: w.own, LinkID: first}, enums.ServiceKimai); id != kimai1 {
		t.Fatalf("chosen: %d", id)
	}

	// Dawarich 1 in two Verbünde with a Kimai each: ambiguous.
	if _, err := verbund.Create(w.d, w.user, "Auftraggeber", []int64{daw1, kimai2}, ""); err != nil {
		t.Fatal(err)
	}
	if _, state := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw1}, enums.ServiceKimai); state != verbund.PartnerAmbiguous {
		t.Fatalf("two verbünde: %s", state)
	}
	if id, _ := partner(t, w, verbund.Asker{SpaceID: w.own, ConnID: daw1, LinkID: first}, enums.ServiceKimai); id != kimai1 {
		t.Fatalf("chosen among two: %d", id)
	}
}

// Without one in the own space, the one the user reaches elsewhere; a
// linked member the user cannot reach is no partner at all.
func TestPartnerAcrossSpaces(t *testing.T) {
	w := newWorld(t)
	ninja := w.conn(t, w.instance, enums.ServiceInvoiceNinja)
	if id, _ := partner(t, w, verbund.Asker{SpaceID: w.own}, enums.ServiceInvoiceNinja); id != ninja {
		t.Fatalf("reached: %d", id)
	}
	w.conn(t, w.instance, enums.ServiceInvoiceNinja)
	if _, state := partner(t, w, verbund.Asker{SpaceID: w.own}, enums.ServiceInvoiceNinja); state != verbund.PartnerAmbiguous {
		t.Fatalf("two reached: %s", state)
	}

	// An instance Paperless linked with a Kimai in someone else's own
	// space: the user sees the Paperless, not that Kimai, and must not get
	// their own Kimai instead.
	boss, bossSpace := testkit.User(t, w.d, "boss@x.de", enums.RoleAdmin)
	hidden := testkit.Conn(t, w.d, boss, bossSpace, enums.ServiceKimai, "https://hidden.example")
	paperless := w.conn(t, w.instance, enums.ServicePaperless)
	w.conn(t, w.own, enums.ServiceKimai)
	id, err := linkrepo.Add(w.d, "Chef", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []linkrepo.Member{{ConnID: paperless, Service: "paperless"}, {ConnID: hidden, Service: "kimai"}} {
		if err := linkrepo.AddMember(w.d, id, m.ConnID, m.Service); err != nil {
			t.Fatal(err)
		}
	}
	if got, state := partner(t, w, verbund.Asker{SpaceID: w.instance, ConnID: paperless}, enums.ServiceKimai); got != 0 || state != verbund.PartnerNone {
		t.Fatalf("unreachable member: %d %s", got, state)
	}
	if _, err := verbund.Get(w.d, w.user, id); !errors.Is(err, verbund.ErrNotFound) {
		t.Fatalf("half-visible Verbund shown: %v", err)
	}
}

// Two Kimai, one Ninja: both pair with it until a Verbund gives the
// Ninja to one of them.
func TestPairs(t *testing.T) {
	w := newWorld(t)
	k1 := w.conn(t, w.own, enums.ServiceKimai)
	k2 := w.conn(t, w.own, enums.ServiceKimai)
	ninja := w.conn(t, w.own, enums.ServiceInvoiceNinja)

	pairs, err := verbund.Pairs(w.d, w.user, []int64{w.own}, enums.ServiceKimai, enums.ServiceInvoiceNinja)
	if err != nil || len(pairs) != 2 {
		t.Fatalf("pairs: %+v %v", pairs, err)
	}
	if _, err := verbund.Create(w.d, w.user, "Firma", []int64{k1, ninja}, ""); err != nil {
		t.Fatal(err)
	}
	pairs, err = verbund.Pairs(w.d, w.user, []int64{w.own}, enums.ServiceKimai, enums.ServiceInvoiceNinja)
	if err != nil || len(pairs) != 1 || pairs[0].A.ID != k1 || pairs[0].B.ID != ninja {
		t.Fatalf("after verbund: %+v %v", pairs, err)
	}
	_ = k2
}

// TestImplicitSettle: a space with one Kimai and one Invoice Ninja has an
// implicit Verbund that knows customers; settling stores it with the
// space's paired connections (not Docker), after which it is gone as
// implicit and listed as stored.
func TestImplicitSettle(t *testing.T) {
	w := newWorld(t)
	kimai := w.conn(t, w.own, enums.ServiceKimai)
	ninja := w.conn(t, w.own, enums.ServiceInvoiceNinja)
	daw := w.conn(t, w.own, enums.ServiceDawarich)
	w.conn(t, w.own, enums.ServiceDocker)

	v, ok, err := verbund.Implicit(w.d, w.user, w.own)
	if err != nil || !ok || !v.HasCustomers() || !v.CanEdit || len(v.Members) != 3 {
		t.Fatalf("implicit %+v ok=%v err=%v", v, ok, err)
	}

	id, err := verbund.Settle(w.d, w.user, w.own, "")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := verbund.Get(w.d, w.user, id)
	if err != nil || !stored.HasService(enums.ServiceKimai) || !stored.HasService(enums.ServiceDawarich) || stored.HasService(enums.ServiceDocker) {
		t.Fatalf("stored %+v err=%v", stored, err)
	}
	for _, m := range stored.Members {
		if m.ConnID != kimai && m.ConnID != ninja && m.ConnID != daw {
			t.Fatalf("unexpected member %+v", m)
		}
	}
	if _, ok, _ := verbund.Implicit(w.d, w.user, w.own); ok {
		t.Fatal("still implicit after settling")
	}
}

// TestImplicitNeedsCustomers: one customer service alone has nothing to
// link; settling then fails.
func TestImplicitNeedsCustomers(t *testing.T) {
	w := newWorld(t)
	w.conn(t, w.own, enums.ServiceKimai)
	w.conn(t, w.own, enums.ServiceDawarich)
	if _, ok, _ := verbund.Implicit(w.d, w.user, w.own); ok {
		t.Fatal("implicit without two customer services")
	}
	if _, err := verbund.Settle(w.d, w.user, w.own, ""); !errors.Is(err, verbund.ErrNoCustomers) {
		t.Fatalf("err = %v, want ErrNoCustomers", err)
	}
}
