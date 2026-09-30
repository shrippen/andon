package connections_test

import (
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/shares"
)

// TestNewHostDropsSecrets: pointing a connection at another host must
// not send the stored token there; the shared one must be entered again
// and the users' own tokens are dropped.
func TestNewHostDropsSecrets(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	member := addUser(t, d, "member@x.de")
	ownerWho, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)

	shared, err := connections.Create(d, ownerWho, space.ID, enums.ServiceKimai, "K", "https://kimai.lan",
		enums.CredentialShared, "team-token", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connections.Update(d, ownerWho, shared, "K", "https://attacker.example", enums.CredentialShared, nil,
		connections.TLSVerify, nil); !errors.Is(err, connections.ErrSecretForHost) {
		t.Fatalf("shared token kept for a new host: %v", err)
	}
	if err := connections.Update(d, ownerWho, shared, "K", "https://kimai.lan/", enums.CredentialShared, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatalf("same host: %v", err)
	}

	personal, err := connections.Create(d, ownerWho, space.ID, enums.ServiceKimai, "P", "https://kimai.lan",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := shares.Grant(d, ownerWho, enums.ResourceConnection, personal, enums.GranteeUser, member.ID, enums.RightUse); err != nil {
		t.Fatal(err)
	}
	memberWho, _ := access.Load(d, member.ID)
	if err := connections.SetMine(d, memberWho, personal, "member-token"); err != nil {
		t.Fatal(err)
	}
	if err := connections.Update(d, ownerWho, personal, "P", "https://attacker.example", enums.CredentialPersonal, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatal(err)
	}
	if c, _ := content.Credential(d, personal, member.ID); c != nil {
		t.Fatal("member's token kept for the new host")
	}
}
