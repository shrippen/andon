package teams_test

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
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/teams"
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

func addUser(t *testing.T, q db.Queryer, email string, role enums.InstanceRole) *model.User {
	t.Helper()
	u := &model.User{Email: email, Name: email, Role: role, IsActive: true,
		Locale: enums.LocaleDE, ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(q, u); err != nil {
		t.Fatalf("add user: %v", err)
	}
	personal := &model.Space{Kind: enums.SpacePersonal, Name: u.Name, OwnerUserID: &u.ID, Version: 1}
	if err := content.AddSpace(q, personal); err != nil {
		t.Fatalf("add personal space: %v", err)
	}
	return u
}

func TestCreateRequiresAdmin(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "u@x.de", enums.RoleUser)
	who, err := access.Load(d, u.ID)
	if err != nil || who == nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := teams.Create(d, who, "IT", ""); !errors.Is(err, teams.ErrDenied) {
		t.Fatalf("expected denied for non-admin, got %v", err)
	}
}

func TestCreateAndOverview(t *testing.T) {
	d := openTestDB(t)
	admin := addUser(t, d, "admin@x.de", enums.RoleAdmin)
	adminWho, _ := access.Load(d, admin.ID)

	teamID, err := teams.Create(d, adminWho, "IT", "1.2.3.4")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	overview, err := teams.Overview(d, adminWho)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	found := false
	for _, v := range overview {
		if v.ID == teamID && v.Name == "IT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected IT team in overview, got %+v", overview)
	}
}

func TestSetMemberAndRemove(t *testing.T) {
	d := openTestDB(t)
	admin := addUser(t, d, "admin@x.de", enums.RoleAdmin)
	member := addUser(t, d, "m@x.de", enums.RoleUser)
	adminWho, _ := access.Load(d, admin.ID)

	teamID, err := teams.Create(d, adminWho, "IT", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := teams.SetMember(d, adminWho, teamID, member.ID, enums.TeamEditor, ""); err != nil {
		t.Fatalf("set member: %v", err)
	}

	memberWho, _ := access.Load(d, member.ID)
	if memberWho.Teams[teamID] != enums.TeamEditor {
		t.Fatalf("expected member to be editor, got %v", memberWho.Teams[teamID])
	}

	// A plain member (not owner, not admin) may not manage others.
	if err := teams.SetMember(d, memberWho, teamID, member.ID, enums.TeamOwner, ""); !errors.Is(err, teams.ErrDenied) {
		t.Fatalf("expected denied for non-owner member, got %v", err)
	}

	if err := teams.RemoveMember(d, adminWho, teamID, member.ID, ""); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	memberWho, _ = access.Load(d, member.ID)
	if _, stillIn := memberWho.Teams[teamID]; stillIn {
		t.Fatal("expected member removed")
	}
}

func TestDeleteRequiresAdmin(t *testing.T) {
	d := openTestDB(t)
	admin := addUser(t, d, "admin@x.de", enums.RoleAdmin)
	user := addUser(t, d, "u@x.de", enums.RoleUser)
	adminWho, _ := access.Load(d, admin.ID)
	userWho, _ := access.Load(d, user.ID)

	teamID, err := teams.Create(d, adminWho, "IT", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := teams.Delete(d, userWho, teamID, ""); !errors.Is(err, teams.ErrDenied) {
		t.Fatalf("expected denied for non-admin delete, got %v", err)
	}
	if err := teams.Delete(d, adminWho, teamID, ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, err := users.Team(d, teamID); err != nil || got != nil {
		t.Fatalf("expected team gone, got %+v err=%v", got, err)
	}
}

// TestCandidatesOnlyForManagers: the list of all users to add (with
// e-mail addresses) is for team owners and admins, not every member.
func TestCandidatesOnlyForManagers(t *testing.T) {
	d := openTestDB(t)
	admin := addUser(t, d, "admin@x.de", enums.RoleAdmin)
	member := addUser(t, d, "m@x.de", enums.RoleUser)
	adminWho, _ := access.Load(d, admin.ID)
	teamID, _ := teams.Create(d, adminWho, "IT", "")
	if err := teams.SetMember(d, adminWho, teamID, member.ID, enums.TeamEditor, ""); err != nil {
		t.Fatal(err)
	}
	memberWho, _ := access.Load(d, member.ID)

	if list, err := teams.Candidates(d, memberWho); err != nil || len(list) != 0 {
		t.Fatalf("member sees %d users, %v", len(list), err)
	}
	list, err := teams.Candidates(d, adminWho)
	if err != nil || len(list) != 2 {
		t.Fatalf("admin: %v %v", list, err)
	}
	overview, _ := teams.Overview(d, memberWho)
	if overview[0].CanManage {
		t.Fatal("editor may manage")
	}
}

// TestLastOwnerStays: a team keeps one owner; removing or demoting the
// last one is refused, a second owner makes it possible.
func TestLastOwnerStays(t *testing.T) {
	d := openTestDB(t)
	admin := addUser(t, d, "admin@x.de", enums.RoleAdmin)
	other := addUser(t, d, "jo@x.de", enums.RoleUser)
	adminWho, _ := access.Load(d, admin.ID)
	teamID, _ := teams.Create(d, adminWho, "IT", "")
	if err := teams.SetMember(d, adminWho, teamID, admin.ID, enums.TeamOwner, ""); err != nil {
		t.Fatal(err)
	}

	if err := teams.RemoveMember(d, adminWho, teamID, admin.ID, ""); !errors.Is(err, teams.ErrLastOwner) {
		t.Fatalf("remove last owner: %v", err)
	}
	if err := teams.SetMember(d, adminWho, teamID, admin.ID, enums.TeamViewer, ""); !errors.Is(err, teams.ErrLastOwner) {
		t.Fatalf("demote last owner: %v", err)
	}
	if err := teams.SetMember(d, adminWho, teamID, other.ID, enums.TeamOwner, ""); err != nil {
		t.Fatal(err)
	}
	if err := teams.RemoveMember(d, adminWho, teamID, admin.ID, ""); err != nil {
		t.Fatalf("remove one of two owners: %v", err)
	}
}
