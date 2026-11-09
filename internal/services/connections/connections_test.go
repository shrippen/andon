package connections_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// addInstance adds the instance space, where templates live.
func addInstance(t *testing.T, q db.Queryer) *model.Space {
	t.Helper()
	sp := &model.Space{Kind: enums.SpaceInstance, Name: "Instanz", Version: 1}
	if err := content.AddSpace(q, sp); err != nil {
		t.Fatalf("add instance space: %v", err)
	}
	return sp
}

// setAdmin makes u an instance admin.
func setAdmin(t *testing.T, q db.Queryer, u *model.User) {
	t.Helper()
	u.Role = enums.RoleAdmin
	if _, err := q.Exec("UPDATE users SET role = ? WHERE id = ?", u.Role, u.ID); err != nil {
		t.Fatalf("set admin: %v", err)
	}
}

func addUser(t *testing.T, q db.Queryer, email string) *model.User {
	t.Helper()
	u := &model.User{Email: email, Name: email, Role: enums.RoleUser, IsActive: true,
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

func TestCreateGetUpdateDelete(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "My Kimai", "https://kimai.example/",
		enums.CredentialShared, "secret-token", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	view, err := connections.Get(d, who, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Name != "My Kimai" || view.URL != "https://kimai.example" || !view.HasSecret {
		t.Fatalf("unexpected view: %+v", view)
	}

	newSecret := "rotated-token"
	if err := connections.Update(d, who, id, "Renamed", "https://kimai.example/v2", enums.CredentialShared,
		&newSecret, connections.TLSVerify, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	view, err = connections.Get(d, who, id)
	if err != nil || view.Name != "Renamed" {
		t.Fatalf("expected rename to persist, got %+v err=%v", view, err)
	}

	if err := connections.Delete(d, who, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := connections.Get(d, who, id); err == nil {
		t.Fatal("expected deleted connection to be gone")
	}
}

func TestActivateAndDeactivate(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	space := addInstance(t, d)
	setAdmin(t, d, u)
	who, _ := access.Load(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	view, _ := connections.Get(d, who, id)
	if view.HasMine {
		t.Fatal("expected no personal credential yet")
	}
	if err := connections.Activate(d, who, id, model.UserHolder(who.UserID), "my-token"); err != nil {
		t.Fatalf("set mine: %v", err)
	}
	view, _ = connections.Get(d, who, id)
	if !view.HasMine {
		t.Fatal("expected personal credential set")
	}
	if err := connections.Deactivate(d, who, id, model.UserHolder(who.UserID)); err != nil {
		t.Fatalf("drop mine: %v", err)
	}
	view, _ = connections.Get(d, who, id)
	if view.HasMine {
		t.Fatal("expected personal credential dropped")
	}
}

func TestTestConnectionReportsMissingCredential(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example",
		enums.CredentialPersonal, "", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := connections.Test(context.Background(), d, who, id)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if result.Ok || result.Message != "credential.missing" {
		t.Fatalf("expected credential.missing, got %+v", result)
	}
}

func TestTestConnectionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version": "2.30.0"}`))
	}))
	defer srv.Close()

	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", srv.URL,
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := connections.Test(context.Background(), d, who, id)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !result.Ok || result.Version != "2.30.0" {
		t.Fatalf("expected successful test with version, got %+v", result)
	}
}

// tokenServer answers the Kimai version check only for the token "tok".
func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "tok") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"version": "2.30.0"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPersonalTokenFromForm: a token entered with a personal connection
// is the creator's own token, so their test succeeds.
func TestPersonalTokenFromForm(t *testing.T) {
	srv := tokenServer(t)
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", srv.URL,
		enums.CredentialPersonal, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result, err := connections.Test(context.Background(), d, who, id); err != nil || !result.Ok {
		t.Fatalf("create personal: expected ok, got %+v err=%v", result, err)
	}

	tok := "tok"
	shared, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai 2", srv.URL,
		enums.CredentialShared, "old", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if err := connections.Update(d, who, shared, "Kimai 2", srv.URL, enums.CredentialPersonal, &tok, connections.TLSVerify, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if result, err := connections.Test(context.Background(), d, who, shared); err != nil || !result.Ok {
		t.Fatalf("update to personal: expected ok, got %+v err=%v", result, err)
	}
}

// TestSwitchToPersonalKeepsEditorToken: switching a shared connection to
// personal without a new token keeps the old one as the editor's own.
func TestSwitchToPersonalKeepsEditorToken(t *testing.T) {
	srv := tokenServer(t)
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", srv.URL,
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := connections.Update(d, who, id, "Kimai", srv.URL, enums.CredentialPersonal, nil, connections.TLSVerify, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if result, err := connections.Test(context.Background(), d, who, id); err != nil || !result.Ok {
		t.Fatalf("expected ok after switch, got %+v err=%v", result, err)
	}
}

// TestHealthStateFollowsLastFetch: a connection whose last fetch worked
// is not failing, however many fetches failed before; it is shaky.
func TestHealthStateFollowsLastFetch(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example/",
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := func() connections.HealthState {
		t.Helper()
		c, err := connections.Get(d, who, id)
		if err != nil {
			t.Fatal(err)
		}
		return c.Health.State()
	}
	now := time.Now().UTC()
	for i := range 3 {
		if err := data.RecordFetch(d, id, now.Add(time.Duration(i)*time.Minute), 10, "HTTP 502"); err != nil {
			t.Fatal(err)
		}
	}
	if got := state(); got != connections.HealthFailing {
		t.Fatalf("after failures: %s", got)
	}
	data.RecordFetch(d, id, now.Add(5*time.Minute), 10, "")
	if got := state(); got != connections.HealthShaky {
		t.Fatalf("after a good test: %s", got)
	}
}

// A connection counts as shaky from 5 % failed fetches; single failures
// among many fetches are noise.
func TestHealthShakyFromFivePercent(t *testing.T) {
	now := time.Now()
	for pct, want := range map[int]connections.HealthState{4: connections.HealthOK, 5: connections.HealthShaky} {
		h := connections.Health{Fetches: 100, FailPct: pct, LastOK: now, LastFail: now.Add(-time.Hour)}
		if got := h.State(); got != want {
			t.Fatalf("%d %%: %s", pct, got)
		}
	}
}

// A service Andon does not know is refused: the form only offers known
// ones, but a posted form or a tool could name any, and such a
// connection never fetches and shows raw text keys.
func TestCreateRefusesUnknownService(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)

	_, err := connections.Create(d, who, space.ID, enums.ServiceType("bogus"), "Bogus", "https://example.org",
		enums.CredentialShared, "x", connections.TLSVerify, nil)
	if !errors.Is(err, connections.ErrUnknownService) {
		t.Fatalf("err = %v, want ErrUnknownService", err)
	}
}

// TestListingQueries: the listing reads all connections, their logins
// and health in a fixed number of statements, not a few per connection
// (each /vat case listed them: 280 ms for the demo).
func TestListingQueries(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	add := func(n int) {
		for range n {
			if _, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example/",
				enums.CredentialPersonal, "token", connections.TLSVerify, nil); err != nil {
				t.Fatalf("create: %v", err)
			}
		}
	}
	cost := func() int {
		count := dbtest.Trace(t, d)
		if _, err := connections.Listing(d, who, enums.RightUse); err != nil {
			t.Fatalf("listing: %v", err)
		}
		return len(count.Statements())
	}

	add(1)
	one := cost()
	add(19)
	if many := cost(); many != one {
		t.Errorf("listing 20 connections: %d statements, 1 connection: %d", many, one)
	}
}

// TestListingMatchesGet: the listing reads health and logins in bulk;
// each row says what Get says of it (fetches, today, errors, own login).
func TestListingMatchesGet(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	now := time.Now().UTC()
	for i, mode := range []enums.CredentialMode{enums.CredentialShared, enums.CredentialPersonal, enums.CredentialShared} {
		id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example/",
			mode, "tok", connections.TLSVerify, nil)
		if err != nil {
			t.Fatal(err)
		}
		// 0: none, 1: two days with a failure, 2: one good fetch today.
		for day := range i * 2 {
			data.RecordFetch(d, id, now.AddDate(0, 0, -day), 20, "")
			data.RecordFetch(d, id, now.AddDate(0, 0, -day).Add(time.Minute), 40, fmt.Sprintf("HTTP 50%d", day))
		}
	}

	list, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("listing: %d connections, want 3", len(list))
	}
	for _, v := range list {
		one, err := connections.Get(d, who, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, one) {
			t.Errorf("connection %d\nlisting %+v\nget     %+v", v.ID, v, one)
		}
	}
}

// TestStripsQueries: the health strips of all connections cost the same
// statements for 1 or 20 connections (admin settings took 89 ms in the
// demo: each strip loaded all widgets of its space again).
func TestStripsQueries(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	now := time.Now().UTC()
	add := func(n int) {
		for range n {
			id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", "https://kimai.example/",
				enums.CredentialShared, "token", connections.TLSVerify, nil)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			data.RecordFetch(d, id, now, 20, "")
		}
	}
	cost := func() int {
		count := dbtest.Trace(t, d)
		if _, err := connections.Strips(d, who, 7, now); err != nil {
			t.Fatalf("strips: %v", err)
		}
		return len(count.Statements())
	}

	add(1)
	one := cost()
	add(19)
	if many := cost(); many != one {
		t.Errorf("strips of 20 connections: %d statements, 1 connection: %d", many, one)
	}
}
