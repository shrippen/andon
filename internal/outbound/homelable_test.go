package outbound_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/outbound"
)

// homelableFake answers like Homelable v3.6.0: local login with a JWT,
// designs, nodes and edges of every canvas in one list each.
type homelableFake struct {
	mu     sync.Mutex
	seq    int
	tokens map[string]bool
	logins int
	items  map[string]map[string]map[string]any // designs, nodes, edges → id → item
	calls  []call
}

const (
	hlUser = "andon"
	hlPass = "pw"
)

func newHomelable(t *testing.T) (*homelableFake, outbound.HomelableAPI) {
	f, api, _ := homelableAt(t)
	return f, api
}

// homelableAt also returns the fake's address.
func homelableAt(t *testing.T) (*homelableFake, outbound.HomelableAPI, string) {
	t.Helper()
	f := &homelableFake{tokens: map[string]bool{}, items: map[string]map[string]map[string]any{"designs": {}, "nodes": {}, "edges": {}}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, outbound.HomelableLive(outbound.HomelableLogin{URL: srv.URL, User: hlUser, Password: hlPass, VerifyTLS: true}), srv.URL
}

func (f *homelableFake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if path == "auth/login" {
		var login map[string]string
		_ = json.Unmarshal(body, &login)
		if login["username"] != hlUser || login["password"] != hlPass {
			reply(w, http.StatusUnauthorized, map[string]any{"detail": "Invalid credentials"})
			return
		}
		f.logins++
		token := "jwt-" + strconv.Itoa(f.logins)
		f.tokens[token] = true
		reply(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer"})
		return
	}
	if !f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		reply(w, http.StatusUnauthorized, map[string]any{"detail": "Could not validate credentials"})
		return
	}

	kind, id, _ := strings.Cut(path, "/")
	items, ok := f.items[kind]
	if !ok {
		reply(w, http.StatusNotFound, nil)
		return
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)

	switch {
	case r.Method == http.MethodGet && id == "":
		list := []any{}
		for _, item := range items {
			list = append(list, item)
		}
		reply(w, http.StatusOK, list)
		return
	case r.Method == http.MethodPost && id == "":
		f.seq++
		item := map[string]any{"id": kind + "-" + strconv.Itoa(f.seq)}
		for k, v := range fields {
			item[k] = v
		}
		// Homelable places a node sent without a position.
		if kind == "nodes" && item["pos_x"] == nil {
			item["pos_x"], item["pos_y"] = 200.0*float64(f.seq), 0.0
		}
		items[item["id"].(string)] = item
		reply(w, http.StatusCreated, item)
		return
	}

	item, ok := items[id]
	if !ok {
		reply(w, http.StatusNotFound, map[string]any{"detail": "Node not found"})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		for k, v := range fields {
			item[k] = v
		}
		reply(w, http.StatusOK, item)
	case http.MethodDelete:
		delete(items, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// put adds an item as if drawn by hand.
func (f *homelableFake) put(kind string, item map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[kind][item["id"].(string)] = item
}

func (f *homelableFake) get(kind, id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items[kind][id]
}

func (f *homelableFake) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.items[kind])
}

// writes are the calls since mark that change something.
func (f *homelableFake) writes(mark int) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls[mark:] {
		if c.Method != http.MethodGet && !strings.HasSuffix(c.Path, "/auth/login") {
			out = append(out, c)
		}
	}
	return out
}

func (f *homelableFake) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *homelableFake) expire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.tokens)
}

const canvasName = "IT-Doku (aus Obsidian)"

// drawing is a zone with a host, two services (one depending on the
// other), a stack without docs and a network.
func drawing() outbound.HomelableDrawing {
	return outbound.HomelableDrawing{Canvas: canvasName,
		Nodes: []outbound.HomelableNode{
			// Children first: the sync orders by depth itself.
			{Key: "note:Immich", Parent: "host:regis", Kind: metrics.GraphService, Label: "Immich",
				Props: []outbound.HomelableProp{{Key: "URL", Value: "fotos.example.org"}}, Check: metrics.CheckHTTPS, CheckTarget: "https://fotos.example.org"},
			{Key: "note:Postgres", Parent: "host:regis", Kind: metrics.GraphService, Label: "Postgres", Stale: true},
			{Key: "stack:regis/kometa", Parent: "host:regis", Kind: metrics.GraphMissing, Label: "kometa"},
			{Key: "host:regis", Parent: "zone:keller", Kind: metrics.GraphHost, Label: "Regis", Hostname: "regis"},
			{Key: "zone:keller", Kind: metrics.GraphZone, Label: "Keller"},
			{Key: "net:Tailscale", Kind: metrics.GraphNetwork, Label: "Tailscale"},
		},
		Edges: []outbound.HomelableEdge{
			{Key: "depends:Immich>Postgres", From: "note:Immich", To: "note:Postgres", Kind: metrics.EdgeDepends},
			{Key: "network:Tailscale>regis", From: "net:Tailscale", To: "host:regis", Kind: metrics.EdgeNetwork},
		},
	}
}

var syncAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func syncOnce(t *testing.T, api outbound.HomelableAPI, d outbound.HomelableDrawing, prev outbound.HomelableState) (outbound.HomelableState, outbound.HomelableLog) {
	t.Helper()
	state, log, err := outbound.SyncHomelable(context.Background(), api, d, prev, syncAt)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(log.Errors) > 0 {
		t.Fatalf("sync errors: %v", log.Errors)
	}
	return state, log
}

func TestHomelableSyncDrawsTheCanvas(t *testing.T) {
	f, api := newHomelable(t)
	// A hand-drawn canvas with a device of its own.
	f.put("designs", map[string]any{"id": "mine", "name": "Netzwerk"})
	f.put("nodes", map[string]any{"id": "router", "design_id": "mine", "label": "Router"})

	state, log := syncOnce(t, api, drawing(), outbound.HomelableState{})
	if log.Created != 8 || log.Updated+log.Removed+log.Unchanged != 0 {
		t.Errorf("log = %+v", log)
	}
	canvas := f.get("designs", state.Canvas)
	if canvas["name"] != canvasName || canvas["design_type"] != "network" {
		t.Errorf("canvas = %v", canvas)
	}

	host := f.get("nodes", state.Nodes["host:regis"].ID)
	if host["parent_id"] != state.Nodes["zone:keller"].ID || host["type"] != "docker_host" || host["container_mode"] != true || host["design_id"] != state.Canvas {
		t.Errorf("host = %v", host)
	}
	immich := f.get("nodes", state.Nodes["note:Immich"].ID)
	if immich["parent_id"] != host["id"] || immich["check_method"] != "https" || immich["check_target"] != "https://fotos.example.org" {
		t.Errorf("immich = %v", immich)
	}
	props, _ := immich["properties"].([]any)
	if len(props) != 1 || props[0].(map[string]any)["key"] != "URL" || props[0].(map[string]any)["visible"] != true {
		t.Errorf("immich properties = %v", props)
	}
	if kometa := f.get("nodes", state.Nodes["stack:regis/kometa"].ID); kometa["custom_colors"] == nil || kometa["check_method"] != nil {
		t.Errorf("kometa = %v", kometa)
	}
	edge := f.get("edges", state.Edges["network:Tailscale>regis"].ID)
	if edge["source"] != state.Nodes["net:Tailscale"].ID || edge["target"] != host["id"] || edge["type"] != "vlan" {
		t.Errorf("network edge = %v", edge)
	}

	// Neither positions nor addresses are ever sent, and the hand-drawn
	// canvas is left alone.
	for _, c := range f.writes(0) {
		for _, field := range []string{"pos_x", "pos_y", "ip", "mac"} {
			if strings.Contains(string(c.Body), `"`+field+`"`) {
				t.Errorf("%s %s sent %s: %s", c.Method, c.Path, field, c.Body)
			}
		}
		if strings.Contains(c.Path, "router") || strings.Contains(c.Path, "mine") {
			t.Errorf("hand-drawn canvas touched: %s %s", c.Method, c.Path)
		}
	}
}

func TestHomelableSecondSyncSendsNothing(t *testing.T) {
	f, api := newHomelable(t)
	state, _ := syncOnce(t, api, drawing(), outbound.HomelableState{})

	// The user rearranges the canvas meanwhile.
	f.get("nodes", state.Nodes["host:regis"].ID)["pos_x"] = 999.0

	mark := f.mark()
	again, log := syncOnce(t, api, drawing(), state)
	if writes := f.writes(mark); len(writes) != 0 {
		t.Errorf("second sync wrote: %+v", writes)
	}
	if log.Unchanged != 8 || log.Created+log.Updated+log.Removed != 0 {
		t.Errorf("log = %+v", log)
	}
	if again.Nodes["host:regis"] != state.Nodes["host:regis"] {
		t.Errorf("state changed: %v → %v", state.Nodes["host:regis"], again.Nodes["host:regis"])
	}
	if f.get("nodes", state.Nodes["host:regis"].ID)["pos_x"] != 999.0 {
		t.Error("layout lost")
	}
}

func TestHomelableSendsOnlyTheChange(t *testing.T) {
	f, api := newHomelable(t)
	state, _ := syncOnce(t, api, drawing(), outbound.HomelableState{})

	d := drawing()
	d.Nodes[1].Stale = false // Postgres reviewed
	mark := f.mark()
	_, log := syncOnce(t, api, d, state)
	writes := f.writes(mark)
	if len(writes) != 1 || writes[0].Method != http.MethodPatch || !strings.HasSuffix(writes[0].Path, state.Nodes["note:Postgres"].ID) {
		t.Fatalf("writes = %+v", writes)
	}
	if log.Updated != 1 || log.Unchanged != 7 {
		t.Errorf("log = %+v", log)
	}
}

func TestHomelableRemovesWhatIsGone(t *testing.T) {
	f, api := newHomelable(t)
	state, _ := syncOnce(t, api, drawing(), outbound.HomelableState{})

	// Postgres' note went deprecated: its node and the edge to it go.
	d := drawing()
	d.Nodes = append(d.Nodes[:1], d.Nodes[2:]...)
	d.Edges = d.Edges[1:]
	mark := f.mark()
	next, log := syncOnce(t, api, d, state)
	if log.Removed != 2 || log.Created+log.Updated != 0 {
		t.Errorf("log = %+v", log)
	}
	deletes := map[string]bool{}
	for _, c := range f.writes(mark) {
		if c.Method != http.MethodDelete {
			t.Errorf("unexpected %s %s", c.Method, c.Path)
		}
		deletes[c.Path] = true
	}
	if !deletes["/api/v1/nodes/"+state.Nodes["note:Postgres"].ID] || !deletes["/api/v1/edges/"+state.Edges["depends:Immich>Postgres"].ID] {
		t.Errorf("deletes = %v", deletes)
	}
	if _, ok := next.Nodes["note:Postgres"]; ok || f.count("nodes") != 5 {
		t.Errorf("postgres kept: state %v, %d nodes", next.Nodes, f.count("nodes"))
	}
}

func TestHomelableRedrawsWhatWasDeletedByHand(t *testing.T) {
	f, api := newHomelable(t)
	state, _ := syncOnce(t, api, drawing(), outbound.HomelableState{})

	gone := state.Nodes["stack:regis/kometa"].ID
	f.mu.Lock()
	delete(f.items["nodes"], gone)
	f.mu.Unlock()

	next, log := syncOnce(t, api, drawing(), state)
	if log.Created != 1 || next.Nodes["stack:regis/kometa"].ID == gone {
		t.Errorf("log = %+v, id %s", log, next.Nodes["stack:regis/kometa"].ID)
	}
}

func TestHomelableSignsInAgainOn401(t *testing.T) {
	f, api := newHomelable(t)
	state, _ := syncOnce(t, api, drawing(), outbound.HomelableState{})
	if f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}

	// The JWT expires after 24 hours.
	f.expire()
	d := drawing()
	d.Nodes[0].Label = "Immich Fotos"
	_, log := syncOnce(t, api, d, state)
	if f.logins != 2 || log.Updated != 1 {
		t.Errorf("logins = %d, log = %+v", f.logins, log)
	}
}

func TestHomelableWrongLoginFails(t *testing.T) {
	_, _, addr := homelableAt(t)
	api := outbound.HomelableLive(outbound.HomelableLogin{URL: addr, User: hlUser, Password: "wrong", VerifyTLS: true})
	_, _, err := outbound.SyncHomelable(context.Background(), api, drawing(), outbound.HomelableState{}, syncAt)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v", err)
	}
}

func TestHomelableDemoAcceptsTheSync(t *testing.T) {
	api := outbound.HomelableDemo("test-demo")
	state, log := syncOnce(t, api, drawing(), outbound.HomelableState{})
	if log.Created != 8 {
		t.Errorf("log = %+v", log)
	}
	if _, log := syncOnce(t, api, drawing(), state); log.Unchanged != 8 {
		t.Errorf("second log = %+v", log)
	}
}
