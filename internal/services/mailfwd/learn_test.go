package mailfwd

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"andon/internal/crypto"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/learn"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// mailFixture is a demo mailbox whose run is stored: Nordhost (UID 1) and
// Farbraum (UID 2), both listed.
func mailFixture(t *testing.T) (*sql.DB, *access.Principal, *model.Connection) {
	t.Helper()
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	id := testkit.Conn(t, d, who, space, enums.ServiceMail, "demo://mail")
	mail, err := connections.ByID(d, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcdata.Get(context.Background(), d, sources.DataKey(enums.ServiceMail), nil, mail, model.UserHolder(who.UserID), svcdata.Force); err != nil {
		t.Fatal(err)
	}
	return d, who, mail
}

// uids are the listed mails in order, with the marked ones.
func uids(t *testing.T, d *sql.DB, who *access.Principal) (order []uint32, marked map[uint32]bool) {
	t.Helper()
	items, _, err := List(context.Background(), d, who)
	if err != nil {
		t.Fatal(err)
	}
	marked = map[uint32]bool{}
	for _, it := range items {
		order = append(order, it.UID)
		marked[it.UID] = it.Unlikely
	}
	return order, marked
}

// "No invoice" hides the mail and teaches the model; undone, the mail
// is back and the model forgets it.
func TestDismissAndRestore(t *testing.T) {
	d, who, mail := mailFixture(t)
	ctx := context.Background()
	examples := func() int {
		return learn.Model(d, PurposeMail, mail, model.UserHolder(who.UserID)).Model.Stats().Examples
	}

	// Undo of a mail not set aside forgets nothing (it may be a forward).
	inv, err := listed(ctx, d, who, mail, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := teach(d, who, mail, inv, metrics.MailIsInvoice); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, d, who, mail.ID, 1, ""); err != nil || examples() != 1 {
		t.Fatalf("undo of a forward: %v, %d examples, want 1", err, examples())
	}

	if err := Dismiss(ctx, d, who, mail.ID, 2, ""); err != nil {
		t.Fatal(err)
	}
	if order, _ := uids(t, d, who); !slices.Equal(order, []uint32{1}) || examples() != 2 {
		t.Fatalf("after dismissing 2: listed %v, %d examples; want [1], 2", order, examples())
	}

	if err := Restore(ctx, d, who, mail.ID, 2, ""); err != nil {
		t.Fatal(err)
	}
	if order, _ := uids(t, d, who); !slices.Equal(order, []uint32{1, 2}) || examples() != 1 {
		t.Fatalf("after restoring 2: listed %v, %d examples; want [1 2], 1", order, examples())
	}
}

// A mail whose sender the model holds for no invoice is listed last and
// marked; the others keep the mailbox's order.
func TestListRanksLearned(t *testing.T) {
	d, who, mail := mailFixture(t)
	ctx := context.Background()
	for uid, k := range map[uint32]metrics.MailKind{1: metrics.MailNoInvoice, 2: metrics.MailIsInvoice} {
		inv, err := listed(ctx, d, who, mail, uid)
		if err != nil {
			t.Fatal(err)
		}
		if err := teach(d, who, mail, inv, k); err != nil {
			t.Fatal(err)
		}
	}

	order, marked := uids(t, d, who)
	if !slices.Equal(order, []uint32{2, 1}) || !marked[1] || marked[2] {
		t.Fatalf("listed %v marked %v, want [2 1] with 1 marked", order, marked)
	}
}

// A mail not in the last run cannot be dismissed; a foreign mailbox not
// at all.
func TestDismissChecks(t *testing.T) {
	d, who, mail := mailFixture(t)
	stranger, _ := testkit.User(t, d, "x@y.z", enums.RoleUser)
	ctx := context.Background()

	if err := Dismiss(ctx, d, who, mail.ID, 99, ""); !errors.Is(err, ErrNoMail) {
		t.Fatalf("unknown mail: %v", err)
	}
	if err := Dismiss(ctx, d, stranger, mail.ID, 1, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("foreign mailbox: %v", err)
	}
}

// A personal mailbox is each holder's own: UID 1 of one is not UID 1 of
// the other, so one setting a mail aside leaves the other's listed.
func TestDismissIsPerHolder(t *testing.T) {
	d := testkit.DB(t)
	space := testkit.Instance(t, d)
	a, _ := testkit.User(t, d, "a@x.de", enums.RoleAdmin)
	b, _ := testkit.User(t, d, "b@x.de", enums.RoleUser)
	id, err := connections.Create(d, a, space, enums.ServiceMail, "Post", "demo://mail", enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := connections.ByID(d, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, who := range []*access.Principal{a, b} {
		secret, err := crypto.Encrypt("demo:demo", crypto.PurposeCredential, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := content.SetCredential(d, id, model.UserHolder(who.UserID), secret, 1, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceMail), nil, mail, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatal(err)
		}
	}

	if err := Dismiss(ctx, d, a, id, 1, ""); err != nil {
		t.Fatal(err)
	}
	if order, _ := uids(t, d, a); slices.Contains(order, 1) {
		t.Fatalf("A still lists his dismissed mail: %v", order)
	}
	if order, _ := uids(t, d, b); !slices.Contains(order, 1) {
		t.Fatalf("B lost his UID 1 to A's dismiss: %v", order)
	}
}
