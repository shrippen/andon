package connections_test

import (
	"errors"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/shares"
)

// TestHookURLNeedsManage: the webhook URL carries its only secret, so
// only who may rotate it (MANAGE) may read it; USE is not enough.
func TestHookURLNeedsManage(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	user := addUser(t, d, "user@x.de")
	ownerWho, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)
	id, err := connections.Create(d, ownerWho, space.ID, enums.ServicePGBackWeb, "Backups", "https://pg.example",
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := shares.Grant(d, ownerWho, enums.ResourceConnection, id, enums.GranteeUser, user.ID, enums.RightUse); err != nil {
		t.Fatal(err)
	}
	userWho, _ := access.Load(d, user.ID)

	if url, err := connections.HookURL(d, ownerWho, id, "https://dash.example"); err != nil || !strings.Contains(url, "/hooks/") {
		t.Fatalf("owner: %q %v", url, err)
	}
	if url, err := connections.HookURL(d, userWho, id, "https://dash.example"); !errors.Is(err, access.ErrDenied) || url != "" {
		t.Fatalf("user with USE: %q %v", url, err)
	}
}
