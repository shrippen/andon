package spaces_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/spaces"
	"andon/internal/services/teams"
	"andon/internal/testkit"
)

// TestTeamSettingsNotForViewers: a team's settings are for its owners
// and editors; a viewer only uses the team's boards.
func TestTeamSettingsNotForViewers(t *testing.T) {
	d := testkit.DB(t)
	team, err := teams.CreateIn(d, "Studio")
	if err != nil {
		t.Fatal(err)
	}
	space, err := content.TeamSpace(d, team.ID)
	if err != nil || space == nil {
		t.Fatalf("team space: %v", err)
	}
	member := func(email string, role enums.TeamRole) *access.Principal {
		who, _ := testkit.User(t, d, email, enums.RoleUser)
		if err := users.SetMember(d, who.UserID, team.ID, role); err != nil {
			t.Fatal(err)
		}
		who, err := access.Load(d, who.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return who
	}
	editor, viewer := member("e@x.de", enums.TeamEditor), member("v@x.de", enums.TeamViewer)

	if err := spaces.OpenSettings(d, editor, space.ID); err != nil {
		t.Fatalf("editor refused: %v", err)
	}
	if err := spaces.OpenSettings(d, viewer, space.ID); err == nil {
		t.Fatal("viewer opens the team settings")
	}
	if _, err := spaces.Settings(d, viewer, space.ID); err == nil {
		t.Fatal("viewer reads the team settings")
	}
	list, err := teams.Overview(d, viewer)
	if err != nil || len(list) != 0 {
		t.Fatalf("viewer sees the team page: %v %v", list, err)
	}
}

// TestInstanceSettingsForAdmins: the instance's settings are for admins;
// a user only uses what the instance offers.
func TestInstanceSettingsForAdmins(t *testing.T) {
	d := testkit.DB(t)
	space := &model.Space{Kind: enums.SpaceInstance, Name: "Instanz", Version: 1}
	if err := content.AddSpace(d, space); err != nil {
		t.Fatalf("instance space: %v", err)
	}
	admin, _ := testkit.User(t, d, "a@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "u@x.de", enums.RoleUser)

	if err := spaces.OpenSettings(d, admin, space.ID); err != nil {
		t.Fatalf("admin refused: %v", err)
	}
	if err := spaces.OpenSettings(d, user, space.ID); err == nil {
		t.Fatal("user opens the instance settings")
	}
}
