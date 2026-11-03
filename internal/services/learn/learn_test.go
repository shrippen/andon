package learn_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
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

	if l := learn.Euer(d, conn); l.Model == nil || l.Model.Stats().Examples != 0 {
		t.Fatalf("before training: %+v", l)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := learn.Overview(d, admin)
	if err != nil || len(rows) != 1 || rows[0].ConnID != conn || rows[0].Stats.Examples == 0 || rows[0].Stats.Labels == 0 {
		t.Fatalf("overview: %+v %v", rows, err)
	}
	l := learn.Euer(d, conn)
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
	if l := learn.Euer(d, conn); l.Model != nil {
		t.Fatal("off still learns")
	}

	if err := learn.Reset(d, admin, learn.PurposeEuer, conn, ""); err != nil {
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
