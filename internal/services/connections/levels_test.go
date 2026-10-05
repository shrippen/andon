package connections_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/teams"
)

// levels is an instance with an admin, a team with its owner and a
// member, and an instance template the admin set up.
type levels struct {
	d                    *sql.DB
	admin, owner, member *access.Principal
	team                 *model.Team
	template             int64
}

func newLevels(t *testing.T) levels {
	t.Helper()
	d := openTestDB(t)
	instance := addInstance(t, d)
	admin := addUser(t, d, "admin@x.de")
	setAdmin(t, d, admin)
	owner := addUser(t, d, "owner@x.de")
	member := addUser(t, d, "member@x.de")

	team, err := teams.CreateIn(d, "Studio")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SetMember(d, owner.ID, team.ID, enums.TeamOwner); err != nil {
		t.Fatal(err)
	}
	if err := users.SetMember(d, member.ID, team.ID, enums.TeamViewer); err != nil {
		t.Fatal(err)
	}

	l := levels{d: d, team: team}
	l.admin, _ = access.Load(d, admin.ID)
	l.owner, _ = access.Load(d, owner.ID)
	l.member, _ = access.Load(d, member.ID)
	l.template, err = connections.Create(d, l.admin, instance.ID, enums.ServiceKimai, "Kimai", "https://kimai.lan",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// conn reads the template as stored.
func (l levels) conn(t *testing.T) *model.Connection {
	t.Helper()
	c, err := content.Connection(l.d, l.template)
	if err != nil || c == nil {
		t.Fatalf("connection: %v", err)
	}
	return c
}

// secret is what a fetch with h's login would send.
func (l levels) secret(t *testing.T, h model.Holder) (string, error) {
	t.Helper()
	return svcdata.Secret(l.d, l.conn(t), h)
}

// TestTemplateEditPauses: an edit of a template pauses every activation
// until its holder activates it again; on the same host the stored login
// stays, so activating needs no new token.
func TestTemplateEditPauses(t *testing.T) {
	l := newLevels(t)
	me := model.UserHolder(l.member.UserID)
	if err := connections.Activate(l.d, l.member, l.template, me, "member-token"); err != nil {
		t.Fatal(err)
	}
	if s, err := l.secret(t, me); err != nil || s != "member-token" {
		t.Fatalf("activated: %q %v", s, err)
	}

	if err := connections.Update(l.d, l.admin, l.template, "Kimai", "https://kimai.lan/v2", enums.CredentialPersonal, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := l.secret(t, me); !errors.Is(err, svcdata.ErrTemplateChanged) {
		t.Fatalf("after the edit: %v", err)
	}
	if v, _ := connections.Get(l.d, l.member, l.template); !v.Paused {
		t.Fatal("view not paused")
	}

	if err := connections.Activate(l.d, l.member, l.template, me, ""); err != nil {
		t.Fatal(err)
	}
	if s, err := l.secret(t, me); err != nil || s != "member-token" {
		t.Fatalf("activated again: %q %v", s, err)
	}
}

// TestRenameKeepsActivations: a template's name only labels it.
func TestRenameKeepsActivations(t *testing.T) {
	l := newLevels(t)
	me := model.UserHolder(l.member.UserID)
	if err := connections.Activate(l.d, l.member, l.template, me, "member-token"); err != nil {
		t.Fatal(err)
	}
	if err := connections.Update(l.d, l.admin, l.template, "Zeiten", "https://kimai.lan", enums.CredentialPersonal, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := l.secret(t, me); err != nil {
		t.Fatalf("renamed: %v", err)
	}
}

// TestTemplateNewHostNeedsLogin: after a move to another host renewing
// the activation without a token fails; the old one must not go there.
func TestTemplateNewHostNeedsLogin(t *testing.T) {
	l := newLevels(t)
	me := model.UserHolder(l.member.UserID)
	if err := connections.Activate(l.d, l.member, l.template, me, "member-token"); err != nil {
		t.Fatal(err)
	}
	if err := connections.Update(l.d, l.admin, l.template, "Kimai", "https://zeit.example", enums.CredentialPersonal, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatal(err)
	}
	if err := connections.Activate(l.d, l.member, l.template, me, ""); !errors.Is(err, svcdata.ErrMissingCredential) {
		t.Fatalf("renewed without a token: %v", err)
	}
	if err := connections.Activate(l.d, l.member, l.template, me, "new-token"); err != nil {
		t.Fatal(err)
	}
	if s, err := l.secret(t, me); err != nil || s != "new-token" {
		t.Fatalf("new login: %q %v", s, err)
	}
}

// TestTeamActivation: a team owner activates an instance template for the
// team; boards in the team space then fetch with the team's login, a
// member's own boards with the member's.
func TestTeamActivation(t *testing.T) {
	l := newLevels(t)
	team := model.TeamHolder(l.team.ID)
	if err := connections.Activate(l.d, l.member, l.template, team, "x"); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("member activated for the team: %v", err)
	}
	if err := connections.Activate(l.d, l.owner, l.template, team, "team-token"); err != nil {
		t.Fatal(err)
	}
	if err := connections.Activate(l.d, l.member, l.template, model.UserHolder(l.member.UserID), "member-token"); err != nil {
		t.Fatal(err)
	}

	instance := svcdata.Place{Kind: enums.SpaceInstance}
	cases := []struct {
		at   svcdata.Place
		want string
	}{
		{svcdata.Place{Kind: enums.SpaceTeam, Team: l.team.ID}, "team-token"},
		{svcdata.Place{Kind: enums.SpacePersonal}, "member-token"},
		{instance, "member-token"},
	}
	for _, c := range cases {
		h := svcdata.HolderAt(instance, c.at, l.member.UserID)
		if s, err := l.secret(t, h); err != nil || s != c.want {
			t.Errorf("at %+v: %q %v, want %q", c.at, s, err, c.want)
		}
	}
}

// TestTeamTemplateNotForTeams: a team's own template takes only its
// members' logins; for one team login it would be a fixed connection.
func TestTeamTemplateNotForTeams(t *testing.T) {
	l := newLevels(t)
	space, _ := content.TeamSpace(l.d, l.team.ID)
	id, err := connections.Create(l.d, l.owner, space.ID, enums.ServiceKimai, "K", "https://kimai.lan",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connections.Activate(l.d, l.owner, id, model.TeamHolder(l.team.ID), "t"); !errors.Is(err, connections.ErrNotTemplate) {
		t.Fatalf("team activated its own template: %v", err)
	}
	if err := connections.Activate(l.d, l.member, id, model.UserHolder(l.member.UserID), "m"); err != nil {
		t.Fatalf("member: %v", err)
	}
}

// TestPersonalIsFixed: a personal space holds only fixed connections; a
// template there would only ever have its owner's login.
func TestPersonalIsFixed(t *testing.T) {
	l := newLevels(t)
	space := access.Personal(l.member)
	id, err := connections.Create(l.d, l.member, space.ID, enums.ServiceKimai, "K", "https://kimai.lan",
		enums.CredentialPersonal, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := connections.Get(l.d, l.member, id)
	if v.Mode != enums.CredentialShared || !v.HasSecret {
		t.Fatalf("personal connection: mode %s, secret %v", v.Mode, v.HasSecret)
	}
	if res, _ := connections.Test(context.Background(), l.d, l.member, id); res.Message == "credential.missing" {
		t.Fatal("own token not used")
	}
}

// TestActivationsShowChanges: a paused activation lists what changed in
// its template, so its holder can decide to activate it again.
func TestActivationsShowChanges(t *testing.T) {
	l := newLevels(t)
	me := model.UserHolder(l.member.UserID)
	if err := connections.Activate(l.d, l.member, l.template, me, "member-token"); err != nil {
		t.Fatal(err)
	}
	if err := connections.Update(l.d, l.admin, l.template, "Kimai", "https://kimai.lan/v2", enums.CredentialPersonal, nil,
		connections.TLSSkip, map[string]any{"all_users": true}); err != nil {
		t.Fatal(err)
	}

	list, err := connections.Activations(l.d, l.member, me)
	if err != nil || len(list) != 1 {
		t.Fatalf("activations: %+v %v", list, err)
	}
	a := list[0]
	if !a.Paused || a.Active || a.NeedsSecret {
		t.Fatalf("state: %+v", a)
	}
	want := []connections.Change{
		{Field: connections.FieldURL, Was: "https://kimai.lan", Now: "https://kimai.lan/v2"},
		{Field: connections.FieldVerifyTLS, Was: "true", Now: "false"},
		{Field: connections.FieldOption, Key: "all_users", Was: "", Now: "true"},
	}
	if !slices.Equal(a.Changes, want) {
		t.Fatalf("changes:\n got %+v\nwant %+v", a.Changes, want)
	}

	if _, err := connections.Activations(l.d, l.member, model.TeamHolder(l.team.ID)); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("member listed the team's: %v", err)
	}
	if list, err := connections.Activations(l.d, l.owner, model.TeamHolder(l.team.ID)); err != nil || len(list) != 1 || list[0].Active {
		t.Fatalf("team: %+v %v", list, err)
	}
}
