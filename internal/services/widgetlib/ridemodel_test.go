package widgetlib_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/learn"
	"andon/internal/services/sites"
	"andon/internal/services/svcdata"
	"andon/internal/services/teams"
	"andon/internal/services/widgetlib"
	"andon/internal/testkit"
	"andon/internal/widgets"
)

// The travel tile gets the viewer's own ride model, the one SetClass
// teaches, also on a team board that reads an instance Dawarich with
// the team's login.
func TestTravelRideModel(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	instance := &model.Space{Kind: enums.SpaceInstance, Name: "Instanz", Version: 1}
	if err := content.AddSpace(d, instance); err != nil {
		t.Fatal(err)
	}
	team, err := teams.CreateIn(d, "Studio")
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.SetMember(d, who, team.ID, who.UserID, enums.TeamOwner, ""); err != nil {
		t.Fatal(err)
	}
	if who, err = access.Load(d, who.UserID); err != nil {
		t.Fatal(err)
	}
	teamSpace, _ := content.TeamSpace(d, team.ID)
	conn := &model.Connection{SpaceID: instance.ID, Key: "dawarich", Name: "Dawarich", Service: string(enums.ServiceDawarich), URL: "http://127.0.0.1:1",
		CredentialMode: enums.CredentialPersonal, VerifyTLS: true, CreatedAt: time.Now().UTC(), Revision: 1}
	if err := content.AddConnection(d, conn); err != nil {
		t.Fatal(err)
	}
	teach := func(m *bayes.Model) bool { return m.Learn("1", "business", []string{"to:area:1"}) }
	if err := learn.Learn(d, sites.PurposeRides, conn, model.UserHolder(who.UserID), teach); err != nil {
		t.Fatal(err)
	}

	id, err := widgetlib.Create(d, who, teamSpace.ID, "travel", "Reisen", nil, &conn.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, _, _ := widgetlib.Detail(d, who, id)
	frag, err := widgetlib.Load(context.Background(), d, who, w, svcdata.Cached)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := frag.Slots[widgets.RideModelSlot].Data.(bayes.Use)
	if !ok || u.Model == nil || u.Model.Stats().Examples != 1 {
		t.Fatalf("the viewer's model: %+v", frag.Slots[widgets.RideModelSlot])
	}
}
