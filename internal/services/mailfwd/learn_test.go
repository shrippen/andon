package mailfwd

import (
	"context"
	"database/sql"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
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

// "No invoice" hides the mail and teaches the model; the next mail of
// that sender is listed last and marked, and forwarding it after all
// teaches the model back.
func TestDismissTeachesAndRanks(t *testing.T) {
	d, who, mail := mailFixture(t)
	ctx := context.Background()

	if err := Dismiss(ctx, d, who, mail.ID, 2, ""); err != nil {
		t.Fatal(err)
	}
	order, _ := uids(t, d, who)
	if len(order) != 1 || order[0] != 1 {
		t.Fatalf("after dismissing 2 listed %v, want [1]", order)
	}
	if got := learn.Model(d, PurposeMail, mail, model.UserHolder(who.UserID)).Model.Stats().Examples; got != 1 {
		t.Fatalf("model holds %d examples, want 1", got)
	}

	// A new mail from the dismissed sender, newest first in the mailbox:
	// the model marks it, it comes last.
	data := lastRun(ctx, d, who, mail)
	next := data.Invoices[1]
	next.UID = 3
	data.Invoices = append([]sources.MailInvoice{next}, data.Invoices...)
	order, marked := uids(t, d, who)
	if len(order) != 2 || order[1] != 3 || !marked[3] || marked[1] {
		t.Fatalf("listed %v marked %v, want 3 last and marked", order, marked)
	}

	// Forwarded after all (Forward's lesson): no longer marked.
	if err := teach(ctx, d, who, mail, 3, metrics.MailIsInvoice); err != nil {
		t.Fatal(err)
	}
	if _, marked := uids(t, d, who); marked[3] {
		t.Fatal("a sender forwarded after all stays marked")
	}
}

// A mail not in the last run cannot be dismissed; a foreign mailbox not
// at all.
func TestDismissChecks(t *testing.T) {
	d, who, mail := mailFixture(t)
	stranger, _ := testkit.User(t, d, "x@y.z", enums.RoleUser)
	ctx := context.Background()

	if err := Dismiss(ctx, d, who, mail.ID, 99, ""); err != ErrNoMail {
		t.Fatalf("unknown mail: %v", err)
	}
	if err := Dismiss(ctx, d, stranger, mail.ID, 1, ""); err != access.ErrDenied {
		t.Fatalf("foreign mailbox: %v", err)
	}
}
