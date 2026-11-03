package learn_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/admin"
	"andon/internal/services/connections"
	"andon/internal/services/learn"
	"andon/internal/testkit"
)

// The job stores what the model learned from the demo's expenses; the
// page reads it with the settings' threshold; off proposes nothing
// learned; reset forgets; only an admin changes anything.
func TestTrainStoreReset(t *testing.T) {
	d := testkit.DB(t)
	admin, space := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	conn := testkit.Conn(t, d, admin, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	ctx := context.Background()
	c, err := content.Connection(d, conn)
	if err != nil {
		t.Fatal(err)
	}
	me := model.UserHolder(admin.UserID)

	if l := learn.Euer(d, c, me); l.Model == nil || l.Model.Stats().Examples != 0 {
		t.Fatalf("before training: %+v", l)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := learn.Overview(d, admin)
	if err != nil || len(rows) != 1 || rows[0].ConnID != conn || rows[0].Stats.Examples == 0 || rows[0].Stats.Labels == 0 {
		t.Fatalf("overview: %+v %v", rows, err)
	}
	l := learn.Euer(d, c, me)
	if l.Model.Stats() != rows[0].Stats || l.MinP != 0.6 {
		t.Fatalf("stored model: %+v, min %v", l.Model.Stats(), l.MinP)
	}

	if err := learn.SaveSettings(d, user, learn.Settings{Mode: learn.ModeOff}, ""); !errors.Is(err, learn.ErrDenied) {
		t.Fatalf("user saves: %v", err)
	}
	if err := learn.SaveSettings(d, admin, learn.Settings{Mode: learn.ModeOff, MinPercent: 10, Hours: 1000}, ""); err != nil {
		t.Fatal(err)
	}
	if s, _ := learn.Load(d); s.Mode != learn.ModeOff || s.MinPercent != 50 || s.Hours != 168 {
		t.Fatalf("clamped: %+v", s)
	}
	if l := learn.Euer(d, c, me); l.Model != nil {
		t.Fatal("off still learns")
	}

	if err := learn.Reset(d, admin, learn.PurposeEuer, conn, model.NoHolder, ""); err != nil {
		t.Fatal(err)
	}
	if rows, _ := learn.Overview(d, admin); len(rows) != 0 {
		t.Fatalf("after reset: %+v", rows)
	}
}

// Each audit action has a text.
func TestAuditNamed(t *testing.T) {
	for _, a := range []string{"learn.settings", "learn.train", "learn.reset"} {
		if !i18n.Has("audit_action." + a) {
			t.Errorf("no text for audit_action.%s", a)
		}
	}
}

// A personal connection learns per holder: each user's own Invoice
// Ninja account, none sees the other's lines.
func TestPersonalPerHolder(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	conn := testkit.Conn(t, d, admin, testkit.Instance(t, d), enums.ServiceInvoiceNinja, "demo://invoiceninja")
	if _, err := d.Exec("UPDATE connections SET credential_mode = 'personal' WHERE id = ?", conn); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*access.Principal{admin, user} {
		if err := connections.Activate(d, who, conn, model.UserHolder(who.UserID), "token"); err != nil {
			t.Fatal(err)
		}
	}

	if err := learn.Train(context.Background(), d, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := learn.Overview(d, admin)
	if err != nil || len(rows) != 2 || rows[0].Holder == rows[1].Holder || rows[0].Whose == "" {
		t.Fatalf("want one model per holder: %+v %v", rows, err)
	}
	c, err := content.Connection(d, conn)
	if err != nil {
		t.Fatal(err)
	}
	if l := learn.Euer(d, c, model.UserHolder(user.UserID)); l.Model.Stats().Examples == 0 {
		t.Fatal("the user reads no model of its own")
	}
	if err := learn.Reset(d, admin, learn.PurposeEuer, conn, model.UserHolder(admin.UserID), ""); err != nil {
		t.Fatal(err)
	}
	if l := learn.Euer(d, c, model.UserHolder(user.UserID)); l.Model.Stats().Examples == 0 {
		t.Fatal("resetting the admin's model reset the user's")
	}
}

// A deleted user's model goes with the user: a new user given the same
// id reads none of it; a login taken back drops its model at training.
func TestHolderGone(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	conn := testkit.Conn(t, d, admin, testkit.Instance(t, d), enums.ServiceInvoiceNinja, "demo://invoiceninja")
	if _, err := d.Exec("UPDATE connections SET credential_mode = 'personal' WHERE id = ?", conn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c, err := content.Connection(d, conn)
	if err != nil {
		t.Fatal(err)
	}

	gone, _ := testkit.User(t, d, "gone@x.de", enums.RoleUser)
	if err := connections.Activate(d, gone, conn, model.UserHolder(gone.UserID), "token"); err != nil {
		t.Fatal(err)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := deleteUser(d, admin, gone.UserID); err != nil {
		t.Fatal(err)
	}
	next, _ := testkit.User(t, d, "next@x.de", enums.RoleUser)
	if next.UserID != gone.UserID {
		t.Skipf("id not reused (%d, %d)", next.UserID, gone.UserID)
	}
	if l := learn.Euer(d, c, model.UserHolder(next.UserID)); l.Model.Stats().Examples != 0 {
		t.Fatalf("new user inherits %d examples", l.Model.Stats().Examples)
	}

	// A login taken back: its model goes at the next training.
	if err := connections.Activate(d, next, conn, model.UserHolder(next.UserID), "token"); err != nil {
		t.Fatal(err)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := connections.Deactivate(d, next, conn, model.UserHolder(next.UserID)); err != nil {
		t.Fatal(err)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rows, _ := learn.Overview(d, admin); len(rows) != 0 {
		t.Fatalf("models without login: %+v", rows)
	}
}

func deleteUser(d *sql.DB, who *access.Principal, id int64) error { return admin.Delete(d, who, id, "") }
