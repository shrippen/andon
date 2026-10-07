package invites_test

import (
	"andon/internal/repos/users"
	"andon/internal/services/auth"
	"errors"
	"path"
	"strconv"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/audit"
	"andon/internal/services/invites"
	"andon/internal/services/mail"
	"andon/internal/settings"
	"andon/internal/testkit"
)

// An invite link works once; only admins create invites.
func TestInviteAcceptOnce(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	if _, err := invites.Create(d, user, "new@x.de", enums.RoleUser, nil, enums.LocaleDE); !errors.Is(err, invites.ErrDenied) {
		t.Fatalf("user invite: %v", err)
	}
	if _, err := invites.Create(d, admin, "user@x.de", enums.RoleUser, nil, enums.LocaleDE); !errors.Is(err, invites.ErrEmailTaken) {
		t.Fatalf("taken email: %v", err)
	}
	link, err := invites.Create(d, admin, "new@x.de", enums.RoleUser, nil, enums.LocaleDE)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := path.Base(link)

	if email, err := invites.Accept(d, token, "New", "a long enough passphrase", enums.LocaleDE); err != nil || email != "new@x.de" {
		t.Fatalf("accept: %q %v", email, err)
	}
	if _, err := invites.Accept(d, token, "Again", "a long enough passphrase", enums.LocaleDE); !errors.Is(err, invites.ErrInviteInvalid) {
		t.Fatalf("second accept: %v", err)
	}
}

// A reset link sets a password once.
func TestResetOnce(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	link, err := invites.AdminResetLink(d, admin, user.UserID)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	token := path.Base(link)
	if ok, _ := invites.ResetValid(d, token); !ok {
		t.Fatal("fresh link must be valid")
	}
	if err := invites.Reset(d, token, "a long enough passphrase", ""); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := invites.Reset(d, token, "another long passphrase", ""); !errors.Is(err, invites.ErrResetInvalid) {
		t.Fatalf("second reset: %v", err)
	}
}

// TestResetRevokesTokens: after a reset (the account may be taken over)
// the API tokens stop working too, as the sessions do.
func TestResetRevokesTokens(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	tok, err := auth.CreateToken(d, user, "cli", enums.TokenRead, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := invites.AdminResetLink(d, admin, user.UserID)
	if err := invites.Reset(d, path.Base(link), "a long enough passphrase", ""); err != nil {
		t.Fatal(err)
	}
	if who, _ := auth.PrincipalForToken(d, tok.Secret, enums.TokenRead); who != nil {
		t.Fatal("token still works after reset")
	}
}

// TestResetRequestsLimited: reset mails to one address (or from one
// client) are capped; the page answers the same either way.
func TestResetRequestsLimited(t *testing.T) {
	d := testkit.DB(t)
	testkit.User(t, d, "user@x.de", enums.RoleUser)
	u, _ := users.ByEmail(d, "user@x.de")
	u.PasswordHash = "x"
	if err := users.Update(d, u); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := invites.RequestReset(d, "user@x.de", "10.9.9."+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM reset_tokens").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 3 {
		t.Fatalf("%d reset mails for one address", n)
	}
}

// TestInviteChecksAddress: no invite to something that is no e-mail
// address; a second invite to the same address replaces the first, so
// only one link stays valid.
func TestInviteChecksAddress(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)

	if _, err := invites.Create(d, admin, "not-an-email", enums.RoleUser, nil, enums.LocaleDE); !errors.Is(err, invites.ErrBadEmail) {
		t.Fatalf("bad address: %v", err)
	}
	first, err := invites.Create(d, admin, "jo@x.de", enums.RoleUser, nil, enums.LocaleDE)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invites.Create(d, admin, "Jo@x.de", enums.RoleUser, nil, enums.LocaleDE); err != nil {
		t.Fatal(err)
	}
	pending, _ := invites.Pending(d, admin)
	if len(pending) != 1 {
		t.Fatalf("pending: %+v", pending)
	}
	if _, err := invites.Accept(d, path.Base(first), "Jo", "a long enough passphrase", enums.LocaleDE); !errors.Is(err, invites.ErrInviteInvalid) {
		t.Fatalf("replaced link still works: %v", err)
	}
}

// The invite says whether its mail went out: none without SMTP, failed
// when the server does not answer, never "sent" regardless.
func TestInviteMailState(t *testing.T) {
	t.Cleanup(func() { mail.Init(settings.Settings{}) })
	mail.Init(settings.Settings{})
	if got := invites.SendMail("a@x.de", "http://x/invite/1", "Ada", enums.LocaleDE); got != invites.MailOff {
		t.Fatalf("without SMTP: %v", got)
	}
	mail.Init(settings.Settings{SMTPURL: "smtp://127.0.0.1:1"})
	if got := invites.SendMail("a@x.de", "http://x/invite/1", "Ada", enums.LocaleDE); got != invites.MailFailed {
		t.Fatalf("SMTP down: %v", got)
	}
}

// The audit names the account a reset link was made for by its address,
// not its id.
func TestAdminResetLinkAuditNamesAddress(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	if _, err := invites.AdminResetLink(d, admin, user.UserID); err != nil {
		t.Fatal(err)
	}
	entries, _ := audit.Entries(d, admin)
	for _, e := range entries {
		if e.Action == "reset.admin_link" && e.Target != "user@x.de" {
			t.Fatalf("target %q", e.Target)
		}
	}
}
