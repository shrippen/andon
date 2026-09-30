package accounts_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	authrepo "andon/internal/repos/auth"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/accounts"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestCreateAlsoCreatesPersonalSpace(t *testing.T) {
	q := openTestDB(t)
	pw := "s3cret-password"
	user, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	sp, err := content.PersonalSpace(q, user.ID)
	if err != nil || sp == nil {
		t.Fatalf("expected personal space, got %+v err=%v", sp, err)
	}
}

func TestCreateRejectsDuplicateEmail(t *testing.T) {
	q := openTestDB(t)
	pw := "s3cret-password"
	if _, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := accounts.Create(q, "A@B.C", "Other", &pw, enums.RoleUser, enums.LocaleDE, ""); !errors.Is(err, accounts.ErrEmailTaken) {
		t.Fatalf("expected email-taken error, got %v", err)
	}
}

func TestCreateRejectsShortPassword(t *testing.T) {
	q := openTestDB(t)
	pw := "short"
	if _, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, ""); !errors.Is(err, accounts.ErrPasswordTooShort) {
		t.Fatalf("expected password-too-short error, got %v", err)
	}
}

func TestChangePasswordRequiresCurrent(t *testing.T) {
	q := openTestDB(t)
	pw := "s3cret-password"
	user, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	who := &access.Principal{UserID: user.ID}

	if err := accounts.ChangePassword(q, who, "wrong", "new-long-password", "1.2.3.4"); !errors.Is(err, accounts.ErrWrongPassword) {
		t.Fatalf("expected wrong-password error, got %v", err)
	}
	if err := accounts.ChangePassword(q, who, pw, "new-long-password", "1.2.3.4"); err != nil {
		t.Fatalf("change password: %v", err)
	}
}

func TestSetPrefRoundTrip(t *testing.T) {
	q := openTestDB(t)
	pw := "s3cret-password"
	user, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	who := &access.Principal{UserID: user.ID}

	if err := accounts.SetPref(q, who, "compact", true); err != nil {
		t.Fatalf("set pref: %v", err)
	}
	p, err := accounts.GetProfile(q, who)
	if err != nil || p.Prefs["compact"] != true {
		t.Fatalf("expected pref round-tripped, got %+v err=%v", p, err)
	}
}

// TestChangePasswordEndsOtherSessions: a new password shuts out whoever
// else was logged in (the reason to change it); this browser stays.
func TestChangePasswordEndsOtherSessions(t *testing.T) {
	q := openTestDB(t)
	pw := "s3cret-password"
	user, err := accounts.Create(q, "a@b.c", "Alex", &pw, enums.RoleUser, enums.LocaleDE, "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var ids []int64
	for _, hash := range []string{"mine", "stolen"} {
		s := &model.LoginSession{TokenHash: hash, UserID: user.ID, Method: enums.AuthPassword, CreatedAt: now, LastSeen: now,
			ExpiresAt: now.Add(time.Hour)}
		if err := authrepo.AddSession(q, s); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	who := &access.Principal{UserID: user.ID, SessionID: &ids[0]}
	if err := accounts.ChangePassword(q, who, pw, "new-long-password", ""); err != nil {
		t.Fatal(err)
	}
	if s, _ := authrepo.SessionByHash(q, "stolen"); s != nil {
		t.Fatal("other session kept")
	}
	if s, _ := authrepo.SessionByHash(q, "mine"); s == nil {
		t.Fatal("own session ended")
	}
}
