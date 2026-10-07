package homelable_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/outbound"
	"andon/internal/services/connections"
	"andon/internal/services/homelable"
	"andon/internal/testkit"
)

// The demo vault (Studio Weber) drawn into the demo's stand-in: the first
// sync creates the canvas, the second finds nothing to change.
func TestSyncDrawsTheDemoVault(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	ctx := context.Background()

	testkit.Conn(t, d, who, space, enums.ServiceGitea, "demo://git")
	id := testkit.Conn(t, d, who, space, enums.ServiceHomelable, "demo://homelable")

	if _, ok, err := homelable.Last(d, who, id); err != nil || ok {
		t.Fatalf("last before any sync: %v %v", ok, err)
	}
	first, err := homelable.Sync(ctx, d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	if first.Problem != "" || first.Created == 0 || len(first.Errors) > 0 || first.Source != "gitea" {
		t.Fatalf("first = %+v", first)
	}

	second, err := homelable.Sync(ctx, d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created+second.Updated+second.Removed != 0 || second.Unchanged != first.Created {
		t.Fatalf("second = %+v after %+v", second, first)
	}
	last, ok, err := homelable.Last(d, who, id)
	if err != nil || !ok || last.Unchanged != second.Unchanged {
		t.Fatalf("last = %+v %v %v", last, ok, err)
	}

	if _, err := homelable.Sync(ctx, d, stranger, id); err == nil {
		t.Fatal("stranger synced")
	}
}

func TestSyncNeedsDocs(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	id := testkit.Conn(t, d, who, space, enums.ServiceHomelable, "demo://homelable")

	log, err := homelable.Sync(context.Background(), d, who, id)
	if err != nil || log.Problem != homelable.ProblemNoDocs {
		t.Fatalf("log = %+v, %v", log, err)
	}
}

// A live connection signs in with its stored user and password and
// remembers the IDs it created.
func TestSyncSignsInWithTheStoredLogin(t *testing.T) {
	var mu sync.Mutex
	var logins []map[string]string
	writes := 0
	store := outbound.HomelableDemo("live-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if path == "auth/login" {
			var login map[string]string
			_ = json.Unmarshal(raw, &login)
			logins = append(logins, login)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			writes++
		}
		var arg any
		if body != nil {
			arg = body
		}
		out, err := store.Send(r.Context(), r.Method, path, arg)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceGitea, "demo://git")
	id, err := connections.Create(d, who, space, enums.ServiceHomelable, "Homelable", srv.URL, enums.CredentialShared, "andon:secret:pw", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := homelable.Sync(context.Background(), d, who, id)
	if err != nil || first.Problem != "" || first.Created == 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if len(logins) != 1 || logins[0]["username"] != "andon" || logins[0]["password"] != "secret:pw" {
		t.Fatalf("logins = %v", logins)
	}
	mu.Lock()
	before := writes
	mu.Unlock()
	if _, err := homelable.Sync(context.Background(), d, who, id); err != nil {
		t.Fatal(err)
	}
	if writes != before {
		t.Fatalf("second sync wrote %d times", writes-before)
	}
}
