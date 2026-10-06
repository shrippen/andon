package sites_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/connections"
	"andon/internal/services/sites"
	"andon/internal/testkit"
)

// write is one request a fake service received.
type write struct {
	Call string
	Body map[string]any
}

type recorder struct {
	mu     sync.Mutex
	writes []write
}

func (r *recorder) add(req *http.Request) {
	var body map[string]any
	json.NewDecoder(req.Body).Decode(&body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, write{req.Method + " " + req.URL.Path, body})
}

func (r *recorder) list() []write {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]write(nil), r.writes...)
}

// fakeDawarich: areas home (1) and Acme (2), no tracks; a new area is 9.
// extra replaces or adds answers by path.
func fakeDawarich(rec *recorder, extra ...map[string]string) *httptest.Server {
	mux := http.NewServeMux()
	bodies := map[string]string{"/api/v1/points": `[]`, "/api/v1/visits": `[]`, "/api/v1/stats": `{}`, "/api/v1/places": `[]`,
		"/api/v1/areas": `[{"id": 1, "name": "Zuhause", "latitude": 52.52, "longitude": 13.4, "radius": 100},
			{"id": 2, "name": "Acme", "latitude": 52.4, "longitude": 13.06, "radius": 100}]`,
		"/api/v1/tracks": `{"features": []}`}
	for _, e := range extra {
		maps.Copy(bodies, e)
	}
	for path, body := range bodies {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
	mux.HandleFunc("POST /api/v1/areas", func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.Write([]byte(`{"id": 9}`))
	})
	return httptest.NewServer(mux)
}

// fakeKimai: customer Acme (12); with plugin a mileage place for home
// (area 1) and one without Dawarich link.
func fakeKimai(rec *recorder, plugin bool) *httptest.Server {
	mux := http.NewServeMux()
	for path, body := range map[string]string{"/api/timesheets": `[]`, "/api/timesheets/active": `[]`, "/api/projects": `[]`,
		"/api/customers": `[{"id": 12, "name": "Acme"}]`} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
	if plugin {
		mux.HandleFunc("GET /api/mileage/ping", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"permissions": {"view": true}, "features": ["places", "placesWrite"]}`))
		})
		mux.HandleFunc("GET /api/mileage/places", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`[{"id": 3, "name": "Zuhause", "type": "home", "dawarichAreaId": 1, "latitude": 52.52, "longitude": 13.4, "radius": 100},
				{"id": 5, "name": "Büro", "type": "work", "latitude": 52.5, "longitude": 13.3, "radius": 150}]`))
		})
		mux.HandleFunc("GET /api/mileage/trips", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
		mux.HandleFunc("POST /api/mileage/places", func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			w.Write([]byte(`{"id": 4}`))
		})
		mux.HandleFunc("PATCH /api/mileage/places/{id}", func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("POST /api/mileage/trips", func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			w.Write([]byte(`{"id": 7}`))
		})
	}
	return httptest.NewServer(mux)
}

// Without the plugin, assignments live in the connection's option.
func TestAssignWithoutPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec), fakeKimai(rec, false)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	ctx := context.Background()

	before, err := sites.Overview(ctx, d, who, conn)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	for _, r := range before.Rows {
		if r.Key == "area:2" && r.Suggest != 12 {
			t.Fatalf("area Acme suggests customer Acme: %+v", r)
		}
	}

	if err := sites.Assign(ctx, d, who, conn, "area:2", metrics.Assignment{Kind: metrics.KindCustomer, CustomerID: 12}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	view, err := sites.Overview(ctx, d, who, conn)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	var acme *sites.Row
	for i := range view.Rows {
		if view.Rows[i].Key == "area:2" {
			acme = &view.Rows[i]
		}
	}
	if acme == nil || acme.Kind != metrics.KindCustomer || acme.CustomerID != 12 || acme.Origin != metrics.OriginAndon {
		t.Fatalf("Acme: %+v", acme)
	}
	if view.Plugin || view.Writes || len(view.Customers) != 1 {
		t.Fatalf("view: %+v", view)
	}
	for _, r := range view.Rows {
		if r.Key == "area:1" && r.Suggest != 0 {
			t.Fatalf("home suggests nobody: %+v", r)
		}
	}
	if w := rec.list(); len(w) != 0 {
		t.Fatalf("no writes without plugin: %v", w)
	}
}

// With the plugin, it stores the assignment; only "private" stays in
// the option.
func TestAssignWithPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec), fakeKimai(rec, true)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	ctx := context.Background()

	if err := sites.Assign(ctx, d, who, conn, "area:2", metrics.Assignment{Kind: metrics.KindCustomer, CustomerID: 12}); err != nil {
		t.Fatalf("assign Acme: %v", err)
	}
	if err := sites.Assign(ctx, d, who, conn, "area:1", metrics.Assignment{Kind: metrics.KindPrivate}); err != nil {
		t.Fatalf("assign home: %v", err)
	}

	w := rec.list()
	if len(w) != 2 || w[0].Call != "POST /api/mileage/places" || w[0].Body["dawarichAreaId"] != 2.0 || w[0].Body["customerId"] != 12.0 || w[0].Body["type"] != "customer" {
		t.Fatalf("writes: %+v", w)
	}
	if w[1].Call != "PATCH /api/mileage/places/3" || w[1].Body["type"] != "other" {
		t.Fatalf("home: %+v", w[1])
	}
	view, _ := connections.Get(d, who, conn)
	places, _ := view.Options["places"].(map[string]any)
	if _, ok := places["area:2"]; ok || places["area:1"] == nil {
		t.Fatalf("option: %v", places)
	}
}

// A new place becomes a Dawarich area and a mileage place linked to it.
func TestCreate(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec), fakeKimai(rec, true)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	p := sites.NewPlace{Name: "Bäcker", Lat: 52.51, Lon: 13.39, Kind: metrics.KindPrivate}
	if err := sites.Create(context.Background(), d, who, conn, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	w := rec.list()
	if len(w) != 2 || w[0].Call != "POST /api/v1/areas" || w[0].Body["name"] != "Bäcker" || w[0].Body["radius"] != 150.0 {
		t.Fatalf("writes: %+v", w)
	}
	if w[1].Call != "POST /api/mileage/places" || w[1].Body["dawarichAreaId"] != 9.0 {
		t.Fatalf("plugin: %+v", w[1])
	}
	if err := sites.Create(context.Background(), d, who, conn, sites.NewPlace{Name: " "}); err != sites.ErrBadPlace {
		t.Fatalf("no name: %v", err)
	}
}

// A sync gives Acme a mileage place and the office a Dawarich area.
func TestSync(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec), fakeKimai(rec, true)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	res, err := sites.Sync(context.Background(), d, who, conn)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.PluginPlaces != 1 || res.Areas != 1 {
		t.Fatalf("result: %+v", res)
	}
	calls := map[string]bool{}
	for _, w := range rec.list() {
		calls[w.Call] = true
	}
	for _, c := range []string{"POST /api/mileage/places", "POST /api/v1/areas", "PATCH /api/mileage/places/5"} {
		if !calls[c] {
			t.Fatalf("missing %s: %v", c, rec.list())
		}
	}
}
