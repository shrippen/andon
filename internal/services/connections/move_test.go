package connections_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

// kimaiAt fakes a Kimai that answers with status and records the
// logins it was sent.
func kimaiAt(t *testing.T, status int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var logins []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins = append(logins, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(`{"version": "2.40"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), logins...)
	}
}

// TestUpdateRefusesNewHost: another server is a move (Move), never a
// plain edit; the same server with another path stays an edit.
func TestUpdateRefusesNewHost(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	who, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)
	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "K", "https://kimai.lan",
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := connections.Update(d, who, id, "K", "https://kimai.example", enums.CredentialShared, nil,
		connections.TLSVerify, nil); !errors.Is(err, connections.ErrMoveHost) {
		t.Fatalf("new host as edit: %v", err)
	}
	if err := connections.Update(d, who, id, "K", "https://kimai.lan/sub/", enums.CredentialShared, nil,
		connections.TLSVerify, nil); err != nil {
		t.Fatalf("same host: %v", err)
	}
}

// TestMoveShared: the stored token goes to the new server only on
// purpose (kept or entered anew); the new address is tested with it
// before anything is saved, and the connection keeps its id, so tiles,
// links and history stay.
func TestMoveShared(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	who, _ := access.Load(d, owner.ID)
	space, _ := content.PersonalSpace(d, owner.ID)
	old, _ := kimaiAt(t, http.StatusOK)
	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "K", old.URL,
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Neither kept nor entered: the stored token must not go there.
	moved, logins := kimaiAt(t, http.StatusOK)
	if _, err := connections.Move(ctx, d, who, id, moved.URL+"/", "", connections.SecretNew, connections.CheckFirst); !errors.Is(err, connections.ErrSecretForHost) {
		t.Fatalf("token moved unasked: %v", err)
	}
	if len(logins()) != 0 {
		t.Fatalf("new server reached: %v", logins())
	}

	// A failed test saves nothing.
	broken, _ := kimaiAt(t, http.StatusUnauthorized)
	result, err := connections.Move(ctx, d, who, id, broken.URL, "", connections.SecretKeep, connections.CheckFirst)
	if err != nil || result.Ok || result.Cause != connections.CauseAuth {
		t.Fatalf("broken target: %+v, %v", result, err)
	}
	if v, _ := connections.Get(d, who, id); v.URL != old.URL {
		t.Fatalf("saved after a failed test: %s", v.URL)
	}

	// Kept on purpose: tested with it, then saved.
	result, err = connections.Move(ctx, d, who, id, moved.URL+"/", "", connections.SecretKeep, connections.CheckFirst)
	if err != nil || !result.Ok {
		t.Fatalf("move: %+v, %v", result, err)
	}
	if got := logins(); len(got) == 0 || !strings.Contains(got[0], "tok") {
		t.Fatalf("test sent %v", got)
	}
	v, err := connections.Get(d, who, id)
	if err != nil || v.URL != moved.URL || !v.HasSecret {
		t.Fatalf("after move: %+v, %v", v, err)
	}

	// Untested on request, with a new token.
	result, err = connections.Move(ctx, d, who, id, "https://kimai.example", "new-tok", connections.SecretNew, connections.CheckSkip)
	if err != nil || !result.Ok {
		t.Fatalf("untested move: %+v, %v", result, err)
	}
	if v, _ := connections.Get(d, who, id); v.URL != "https://kimai.example" {
		t.Fatalf("untested move not saved: %s", v.URL)
	}
}

// TestMovePausesHolders: a template's holders keep their logins on a
// move, paused until each sends theirs to the new server (Activate
// without a secret), instead of losing them.
func TestMovePausesHolders(t *testing.T) {
	d := openTestDB(t)
	owner := addUser(t, d, "owner@x.de")
	member := addUser(t, d, "member@x.de")
	instance := addInstance(t, d)
	setAdmin(t, d, owner)
	ownerWho, _ := access.Load(d, owner.ID)
	id, err := connections.Create(d, ownerWho, instance.ID, enums.ServiceKimai, "P", "https://kimai.lan",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	memberWho, _ := access.Load(d, member.ID)
	mine := model.UserHolder(member.ID)
	if err := connections.Activate(d, memberWho, id, mine, "member-token"); err != nil {
		t.Fatal(err)
	}

	if _, err := connections.Move(context.Background(), d, ownerWho, id, "https://kimai.example", "", connections.SecretNew, connections.CheckSkip); err != nil {
		t.Fatal(err)
	}
	list, err := connections.ActivationsOf(d, memberWho, id)
	if err != nil || len(list) != 1 {
		t.Fatalf("activations: %+v, %v", list, err)
	}
	if a := list[0]; !a.Paused || a.NeedsSecret || !a.NewHost {
		t.Fatalf("member's login after the move: %+v", a)
	}

	if err := connections.Activate(d, memberWho, id, mine, ""); err != nil {
		t.Fatalf("release the stored login: %v", err)
	}
	if list, _ := connections.ActivationsOf(d, memberWho, id); !list[0].Active {
		t.Fatalf("still paused: %+v", list[0])
	}
}
