package timer_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/timer"
	"andon/internal/testkit"
	"andon/internal/widgets"
)

// A timer tile writes to Kimai; bad ranges and other tiles are refused
// before anything is sent.
func TestRunWritesOnlyForTimers(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)

	var mu sync.Mutex
	var writes []string
	kimai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	tile := testkit.Place(t, d, who, space, timer.WidgetType, nil, &conn)
	ctx := context.Background()

	if err := timer.Run(ctx, d, who, tile, timer.Request{Action: timer.ActionStart, Project: 3, Activity: 7}, ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	bad := timer.Request{Action: timer.ActionCreate, Project: 3, Activity: 7, Begin: "2026-09-26T10:00", End: "2026-09-26T09:00"}
	if err := timer.Run(ctx, d, who, tile, bad, ""); !errors.Is(err, timer.ErrBadRange) {
		t.Fatalf("bad range: %v", err)
	}
	note := testkit.Place(t, d, who, space, "note", nil, nil)
	if err := timer.Run(ctx, d, who, note, timer.Request{Action: timer.ActionStart, Project: 3, Activity: 7}, ""); !errors.Is(err, timer.ErrNotTimer) {
		t.Fatalf("note tile: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 1 || writes[0] != "POST /api/timesheets" {
		t.Fatalf("writes: %v", writes)
	}
}

// A start can carry a description for the new timesheet.
func TestStartWithDescription(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	var body map[string]any
	kimai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{}`))
	}))
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	tile := testkit.Place(t, d, who, space, timer.WidgetType, nil, &conn)
	req := timer.Request{Action: timer.ActionStart, Project: 3, Activity: 7, StartNote: "Angebot"}
	if err := timer.Run(context.Background(), d, who, tile, req, ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	if body["description"] != "Angebot" {
		t.Fatalf("body: %v", body)
	}
}

// kimaiWrites is a Kimai stub: today's list holds sheet 5 (09:00–12:00,
// billable, tagged), recent lists 3/7, writes are recorded with bodies.
type kimaiWrites struct {
	mu     sync.Mutex
	writes []string
	bodies []map[string]any
	reject func(body map[string]any) bool // answer 400
}

func (k *kimaiWrites) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/api/timesheets":
			w.Header().Set("X-Total-Pages", "1")
			w.Write([]byte(`[{"id": 5, "begin": "2026-09-29T09:00:00+0200", "end": "2026-09-29T12:00:00+0200", "description": "Schnitt",
				"tags": ["Film"], "billable": true, "project": {"id": 3, "name": "Relaunch"}, "activity": {"id": 7, "name": "Dev"}}]`))
		case "/api/timesheets/recent":
			w.Write([]byte(`[{"id": 4, "project": {"id": 3, "name": "Relaunch", "customer": {"name": "Acme"}}, "activity": {"id": 7, "name": "Dev"}}]`))
		default:
			w.Write([]byte(`[]`))
		}
		return
	}

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.writes = append(k.writes, r.Method+" "+r.URL.Path)
	k.bodies = append(k.bodies, body)
	if k.reject != nil && k.reject(body) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Write([]byte(`{}`))
}

func kimaiTile(t *testing.T, k *kimaiWrites) (*sql.DB, *access.Principal, int64) {
	t.Helper()
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	kimai := httptest.NewServer(k)
	t.Cleanup(kimai.Close)
	conn := testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	return d, who, testkit.Place(t, d, who, space, timer.WidgetType, nil, &conn)
}

// Editing the running entry sends begin, tags and billable, no end.
func TestEditRunning(t *testing.T) {
	k := &kimaiWrites{}
	d, who, tile := kimaiTile(t, k)
	req := timer.Request{Action: timer.ActionEdit, Sheet: 9, Project: 3, Activity: 7, Begin: "2026-09-29T08:00", Note: "Kunde",
		Tags: " Film, ,Ton ", Billable: enums.BillableYes}
	if err := timer.Run(context.Background(), d, who, tile, req, ""); err != nil {
		t.Fatalf("edit: %v", err)
	}
	// Tags "Film" and "Ton" are created first.
	body := k.bodies[2]
	if k.writes[2] != "PATCH /api/timesheets/9" || body["begin"] != "2026-09-29T08:00:00" || body["end"] != nil ||
		body["tags"] != "Film,Ton" || body["billable"] != true || body["description"] != "Kunde" {
		t.Fatalf("edit: %v %v", k.writes, body)
	}
}

// Kimai drops tags it does not know, so each one is created first; a
// tag that exists already (400) does not stop the write.
func TestTagsCreated(t *testing.T) {
	k := &kimaiWrites{reject: func(body map[string]any) bool { return body["name"] == "Film" }}
	d, who, tile := kimaiTile(t, k)
	req := timer.Request{Action: timer.ActionCreate, Project: 3, Activity: 7, Begin: "2026-09-29T08:00", End: "2026-09-29T09:00",
		Tags: "Film, Neu"}
	if err := timer.Run(context.Background(), d, who, tile, req, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := []string{"POST /api/tags", "POST /api/tags", "POST /api/timesheets"}
	if !slices.Equal(k.writes, want) || k.bodies[0]["name"] != "Film" || k.bodies[1]["name"] != "Neu" || k.bodies[1]["visible"] != true {
		t.Fatalf("writes: %v %v", k.writes, k.bodies)
	}
}

// A split ends the sheet at the cut and books the rest with the same
// fields; a cut outside the sheet sends nothing.
func TestSplitAndDelete(t *testing.T) {
	k := &kimaiWrites{}
	d, who, tile := kimaiTile(t, k)
	ctx := context.Background()

	outside := timer.Request{Action: timer.ActionSplit, Sheet: 5, At: "2026-09-29T12:30"}
	if err := timer.Run(ctx, d, who, tile, outside, ""); !errors.Is(err, timer.ErrBadSplit) || len(k.writes) != 0 {
		t.Fatalf("outside: %v %v", err, k.writes)
	}
	if err := timer.Run(ctx, d, who, tile, timer.Request{Action: timer.ActionSplit, Sheet: 5, At: "2026-09-29T10:30"}, ""); err != nil {
		t.Fatalf("split: %v", err)
	}
	first, second := k.bodies[0], k.bodies[1]
	if k.writes[0] != "PATCH /api/timesheets/5" || first["end"] != "2026-09-29T10:30:00" || first["billable"] != nil || first["description"] != "Schnitt" {
		t.Fatalf("first: %v %v", k.writes, first)
	}
	if k.writes[1] != "POST /api/timesheets" || second["begin"] != "2026-09-29T10:30:00" || second["end"] != "2026-09-29T12:00:00" ||
		second["billable"] != true || second["tags"] != "Film" || second["project"] != 3.0 {
		t.Fatalf("second: %v %v", k.writes, second)
	}

	if err := timer.Run(ctx, d, who, tile, timer.Request{Action: timer.ActionDelete, Sheet: 5}, ""); err != nil || k.writes[2] != "DELETE /api/timesheets/5" {
		t.Fatalf("delete: %v %v", err, k.writes)
	}
}

// Without the billable permission Kimai rejects the field; the write is
// sent again without it. Other rejections reach the form.
func TestBillableFallback(t *testing.T) {
	k := &kimaiWrites{reject: func(body map[string]any) bool { _, ok := body["billable"]; return ok }}
	d, who, tile := kimaiTile(t, k)
	req := timer.Request{Action: timer.ActionCreate, Project: 3, Activity: 7, Begin: "2026-09-29T08:00", End: "2026-09-29T09:00",
		Billable: enums.BillableNo}
	if err := timer.Run(context.Background(), d, who, tile, req, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(k.writes) != 2 || k.bodies[1]["billable"] != nil {
		t.Fatalf("retry: %v %v", k.writes, k.bodies)
	}

	k.reject = func(map[string]any) bool { return true }
	if err := timer.Run(context.Background(), d, who, tile, req, ""); !errors.Is(err, timer.ErrRejected) {
		t.Fatalf("rejected: %v", err)
	}
}

// Pinning takes the names from Kimai's recent list; pinning again unpins,
// and a pair Kimai does not list is refused.
func TestPin(t *testing.T) {
	d, who, tile := kimaiTile(t, &kimaiWrites{})
	ctx := context.Background()
	favs := func() []widgets.KimaiFav {
		u, err := users.Get(d, who.UserID)
		if err != nil {
			t.Fatal(err)
		}
		all, _ := u.Prefs[widgets.KimaiFavsPref].(map[string]any)
		for _, list := range all {
			return widgets.KimaiFavsOf(list)
		}
		return nil
	}

	if err := timer.Pin(ctx, d, who, tile, 3, 7); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if f := favs(); len(f) != 1 || f[0].Project != "Relaunch" || f[0].Customer != "Acme" {
		t.Fatalf("pinned: %+v", f)
	}
	if err := timer.Pin(ctx, d, who, tile, 4, 4); !errors.Is(err, timer.ErrNotTimer) {
		t.Fatalf("unknown pair: %v", err)
	}
	if err := timer.Pin(ctx, d, who, tile, 3, 7); err != nil || len(favs()) != 0 {
		t.Fatalf("unpin: %v %+v", err, favs())
	}
}

// The edit form starts from the sheet in Kimai's clock.
func TestDraft(t *testing.T) {
	d, who, tile := kimaiTile(t, &kimaiWrites{})
	req, err := timer.Draft(context.Background(), d, who, tile, 5)
	if err != nil {
		t.Fatal(err)
	}
	if req.Begin != "2026-09-29T09:00" || req.End != "2026-09-29T12:00" || req.Tags != "Film" || req.Billable != enums.BillableYes || req.Note != "Schnitt" {
		t.Fatalf("draft: %+v", req)
	}
	if _, err := timer.Draft(context.Background(), d, who, tile, 99); !errors.Is(err, timer.ErrNotTimer) {
		t.Fatalf("unknown sheet: %v", err)
	}
}

// Using a connection is not enough to write to it: a plain user of the
// instance space may see the timer but not start it.
func TestRunNeedsEditRight(t *testing.T) {
	d := testkit.DB(t)
	instance := testkit.Instance(t, d)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	writes := 0
	kimai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes++
		w.Write([]byte(`{}`))
	}))
	defer kimai.Close()
	conn := testkit.Conn(t, d, boss, instance, enums.ServiceKimai, kimai.URL)
	tile := testkit.Place(t, d, boss, instance, timer.WidgetType, nil, &conn)

	err := timer.Run(context.Background(), d, user, tile, timer.Request{Action: timer.ActionStart, Project: 3, Activity: 7}, "")
	if !errors.Is(err, access.ErrDenied) || writes != 0 {
		t.Fatalf("start with USE: %v, %d writes", err, writes)
	}
}
