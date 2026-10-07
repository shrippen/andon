package mailfwd_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/mailfwd"
	"andon/internal/testkit"
)

// Forwarding needs a usable mailbox and a Paperless next to it; both are
// checked before any mail is fetched.
func TestForwardChecksBeforeFetching(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "x@y.z", enums.RoleUser)
	box := testkit.Conn(t, d, who, space, enums.ServiceMail, "imaps://mail.example:993")
	ctx := context.Background()

	if _, err := mailfwd.Forward(ctx, d, who, box, 1, ""); !errors.Is(err, mailfwd.ErrNoPaperless) {
		t.Fatalf("without Paperless: %v", err)
	}
	if _, err := mailfwd.Forward(ctx, d, stranger, box, 1, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("foreign mailbox: %v", err)
	}
}

// Forwarding writes to Paperless: using the mailbox is not enough.
func TestForwardNeedsEditRight(t *testing.T) {
	d := testkit.DB(t)
	instance := testkit.Instance(t, d)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	box := testkit.Conn(t, d, boss, instance, enums.ServiceMail, "imaps://mail.example:993")
	testkit.Conn(t, d, boss, instance, enums.ServicePaperless, "https://docs.example")

	if _, err := mailfwd.Forward(context.Background(), d, user, box, 1, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("forward with USE: %v", err)
	}
}

// Without the own Paperless login, Forward names that connection, before
// any mail is fetched.
func TestForwardNamesMissingLogin(t *testing.T) {
	d := testkit.DB(t)
	space := testkit.Instance(t, d)
	who, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	box := testkit.Conn(t, d, who, space, enums.ServiceMail, "imaps://mail.example:993")
	docs, err := connections.Create(d, who, space, enums.ServicePaperless, "Archiv", "https://docs.example",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = mailfwd.Forward(context.Background(), d, who, box, 1, "")
	var missing *mailfwd.LoginError
	if !errors.As(err, &missing) || missing.ConnID != docs {
		t.Fatalf("want the login of %d missing, got %v", docs, err)
	}
}

// Demo connections take no writes.
func TestForwardRefusesDemo(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	box := testkit.Conn(t, d, who, space, enums.ServiceMail, "demo://mail")
	testkit.Conn(t, d, who, space, enums.ServicePaperless, "demo://paperless")

	if _, err := mailfwd.Forward(context.Background(), d, who, box, 1, ""); !errors.Is(err, mailfwd.ErrDemo) {
		t.Fatalf("demo: %v", err)
	}
}
