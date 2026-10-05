package spaces_test

import (
	"testing"

	"andon/internal/enums"
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
