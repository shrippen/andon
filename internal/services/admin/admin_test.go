package admin_test

import (
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	authrepo "andon/internal/repos/auth"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/accounts"
	"andon/internal/services/admin"
	"andon/internal/services/system"
	"andon/internal/testkit"
)

// The instance never loses its last admin, and only admins manage users.
func TestLastAdminStays(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	if _, err := admin.Users(d, user); !errors.Is(err, admin.ErrDenied) {
		t.Fatalf("user lists users: %v", err)
	}
	if err := admin.SetRole(d, boss, boss.UserID, enums.RoleUser, ""); !errors.Is(err, admin.ErrLastAdmin) {
		t.Fatalf("demote last admin: %v", err)
	}
	if err := admin.SetActive(d, boss, boss.UserID, admin.Off, ""); err == nil {
		t.Fatal("an admin must not disable themselves")
	}

	if err := admin.SetRole(d, boss, user.UserID, enums.RoleAdmin, ""); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if err := admin.SetRole(d, boss, boss.UserID, enums.RoleUser, ""); err != nil {
		t.Fatalf("demote with a second admin: %v", err)
	}
}

// strongPassword satisfies the password rules.
const strongPassword = "correct-horse-battery"

// Only admins switch accounts on or off; disabling ends the sessions.
func TestSetActiveDisablesAndEndsSessions(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	other, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)

	now := time.Now().UTC()
	session := &model.LoginSession{TokenHash: "h1", UserID: user.UserID, Method: enums.AuthPassword,
		CSRF: "c", CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(time.Hour)}
	if err := authrepo.AddSession(d, session); err != nil {
		t.Fatal(err)
	}

	if err := admin.SetActive(d, other, user.UserID, admin.Off, ""); !errors.Is(err, admin.ErrDenied) {
		t.Fatalf("non-admin disables: %v", err)
	}
	if err := admin.SetActive(d, boss, 9999, admin.Off, ""); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if err := admin.SetActive(d, boss, user.UserID, admin.Off, ""); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if u, _ := users.Get(d, user.UserID); u.IsActive {
		t.Fatal("user still active")
	}
	if left, _ := authrepo.SessionsOf(d, user.UserID); len(left) != 0 {
		t.Fatalf("sessions survive disable: %d", len(left))
	}

	if err := admin.SetActive(d, boss, user.UserID, admin.On, ""); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if u, _ := users.Get(d, user.UserID); !u.IsActive {
		t.Fatal("user not re-enabled")
	}
}

// A second admin can be disabled, but not the last active one.
func TestSetActiveKeepsLastActiveAdmin(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	second, _ := testkit.User(t, d, "admin2@x.de", enums.RoleAdmin)

	if err := admin.SetActive(d, boss, second.UserID, admin.Off, ""); err != nil {
		t.Fatalf("disable second admin: %v", err)
	}
	if err := admin.SetActive(d, boss, boss.UserID, admin.Off, ""); !errors.Is(err, admin.ErrSelf) {
		t.Fatalf("disable self: %v", err)
	}

	// second's principal was loaded while active and still carries the
	// admin role: it must not switch off the only active admin.
	if err := admin.SetActive(d, second, boss.UserID, admin.Off, ""); !errors.Is(err, admin.ErrLastAdmin) {
		t.Fatalf("disable last active admin: %v", err)
	}
}

// Break-glass access is an admin-only flag that toggles both ways.
func TestSetBreakglassTogglesFlag(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	if err := admin.SetBreakglass(d, user, user.UserID, admin.On, ""); !errors.Is(err, admin.ErrDenied) {
		t.Fatalf("non-admin sets break-glass: %v", err)
	}
	if err := admin.SetBreakglass(d, boss, 9999, admin.On, ""); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}

	if err := admin.SetBreakglass(d, boss, user.UserID, admin.On, ""); err != nil {
		t.Fatalf("on: %v", err)
	}
	if u, _ := users.Get(d, user.UserID); !u.IsBreakglass {
		t.Fatal("break-glass not set")
	}

	if err := admin.SetBreakglass(d, boss, user.UserID, admin.Off, ""); err != nil {
		t.Fatalf("off: %v", err)
	}
	if u, _ := users.Get(d, user.UserID); u.IsBreakglass {
		t.Fatal("break-glass not cleared")
	}
}

// Delete removes the account and its personal space; nobody deletes
// themselves, and non-admins delete nobody.
func TestDeleteRemovesUserAndSpace(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	other, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)

	if err := admin.Delete(d, other, user.UserID, ""); !errors.Is(err, admin.ErrDenied) {
		t.Fatalf("non-admin deletes: %v", err)
	}
	if err := admin.Delete(d, boss, boss.UserID, ""); !errors.Is(err, admin.ErrSelf) {
		t.Fatalf("delete self: %v", err)
	}
	if err := admin.Delete(d, boss, 9999, ""); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}

	if err := admin.Delete(d, boss, user.UserID, ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if u, _ := users.Get(d, user.UserID); u != nil {
		t.Fatal("user survives delete")
	}
	if sp, _ := content.PersonalSpace(d, user.UserID); sp != nil {
		t.Fatal("personal space survives delete")
	}
}

// The last active admin cannot be deleted by a (disabled) fellow admin.
func TestDeleteKeepsLastActiveAdmin(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	second, _ := testkit.User(t, d, "admin2@x.de", enums.RoleAdmin)
	if err := admin.SetActive(d, boss, second.UserID, admin.Off, ""); err != nil {
		t.Fatal(err)
	}

	if err := admin.Delete(d, second, boss.UserID, ""); !errors.Is(err, admin.ErrLastAdmin) {
		t.Fatalf("delete last active admin: %v", err)
	}
}

// A disabled admin does not count as active: deleting it leaves the
// active admin, so the last-admin guard must not refuse.
func TestDeleteDisabledAdminAllowed(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	second, _ := testkit.User(t, d, "admin2@x.de", enums.RoleAdmin)
	if err := admin.SetActive(d, boss, second.UserID, admin.Off, ""); err != nil {
		t.Fatal(err)
	}

	if err := admin.Delete(d, boss, second.UserID, ""); err != nil {
		t.Fatalf("delete disabled admin: %v", err)
	}
}

// Demoting a disabled admin leaves the active admin, so the last-admin
// guard must not refuse.
func TestDemoteDisabledAdminAllowed(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	second, _ := testkit.User(t, d, "admin2@x.de", enums.RoleAdmin)
	if err := admin.SetActive(d, boss, second.UserID, admin.Off, ""); err != nil {
		t.Fatal(err)
	}

	if err := admin.SetRole(d, boss, second.UserID, enums.RoleUser, ""); err != nil {
		t.Fatalf("demote disabled admin: %v", err)
	}
}

// Self-registration works only while an admin keeps it open, and always
// creates a plain, active user with a personal space.
func TestRegisterFollowsSwitch(t *testing.T) {
	d := testkit.DB(t)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)

	if _, err := admin.Register(d, "new@x.de", "New", strongPassword, enums.LocaleDE); !errors.Is(err, admin.ErrClosed) {
		t.Fatalf("closed registration: %v", err)
	}
	if u, _ := users.ByEmail(d, "new@x.de"); u != nil {
		t.Fatal("closed registration created a user")
	}

	if err := system.Put(d, boss, system.RegistrationKey, map[string]any{"open": true}, ""); err != nil {
		t.Fatal(err)
	}
	email, err := admin.Register(d, " new@x.de ", "New", strongPassword, enums.LocaleDE)
	if err != nil || email != "new@x.de" {
		t.Fatalf("open registration: %q, %v", email, err)
	}

	u, _ := users.ByEmail(d, "new@x.de")
	if u == nil || u.Role != enums.RoleUser || !u.IsActive {
		t.Fatalf("registered user: %+v", u)
	}
	if sp, _ := content.PersonalSpace(d, u.ID); sp == nil {
		t.Fatal("registered user has no personal space")
	}

	if _, err := admin.Register(d, "new@x.de", "Again", strongPassword, enums.LocaleDE); !errors.Is(err, accounts.ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}
	if _, err := admin.Register(d, "short@x.de", "Short", "short", enums.LocaleDE); !errors.Is(err, accounts.ErrPasswordTooShort) {
		t.Fatalf("short password: %v", err)
	}
}
