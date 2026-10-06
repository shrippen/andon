package connections_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/testkit"
)

// oldMileage is a Kimai whose mileage plugin lets the token edit its
// trips but has no placesWrite yet.
func oldMileage(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	for path, body := range map[string]string{"/api/timesheets": `[]`, "/api/timesheets/active": `[]`, "/api/projects": `[]`, "/api/customers": `[]`,
		"/api/mileage/ping":   `{"permissions": {"view": true, "editOwn": true}, "features": ["places"]}`,
		"/api/mileage/places": `[]`, "/api/mileage/trips": `[]`} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The record lists every declared capability: unknown before the first
// fetch, then had or missing with the need.
func TestCapabilities(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	id := testkit.Conn(t, d, who, space, enums.ServiceKimai, oldMileage(t).URL)
	ctx := context.Background()

	rows, err := connections.Capabilities(ctx, d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(caps.Declared(caps.HolderOf(enums.ServiceKimai))) || rows[0].State != connections.CapUnknown {
		t.Fatalf("before fetch: %+v", rows)
	}

	conn, err := connections.ByID(d, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcdata.Get(ctx, d, "kimai.data", nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
		t.Fatal(err)
	}
	rows, err = connections.Capabilities(ctx, d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[caps.Domain]map[caps.Op]connections.CapRow{}
	for _, r := range rows {
		if got[r.Cap.Domain] == nil {
			got[r.Cap.Domain] = map[caps.Op]connections.CapRow{}
		}
		got[r.Cap.Domain][r.Cap.Op] = r
	}
	if got[caps.Places][caps.Read].State != connections.CapHave || got[caps.Rides][caps.Update].State != connections.CapHave {
		t.Fatalf("had: %+v", got)
	}
	if r := got[caps.Places][caps.Update]; r.State != connections.CapMissing || r.Need.Name != "placesWrite" {
		t.Fatalf("places update: %+v", r)
	}
}

// Services without declarations show nothing; others need USE.
func TestCapabilitiesNoneAndDenied(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	other, _ := testkit.User(t, d, "x@b.c", enums.RoleUser)
	wallos := testkit.Conn(t, d, who, space, enums.ServiceWallos, "https://w.example")
	if rows, err := connections.Capabilities(context.Background(), d, who, wallos); err != nil || rows != nil {
		t.Fatalf("wallos: %+v %v", rows, err)
	}
	if _, err := connections.Capabilities(context.Background(), d, other, wallos); !errors.Is(err, access.ErrDenied) && !errors.Is(err, connections.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
}
