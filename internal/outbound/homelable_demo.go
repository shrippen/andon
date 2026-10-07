package outbound

// The demo's Homelable: an in-memory canvas store that accepts every
// sync of a demo:// connection, so its record shows a plausible log. It
// lives as long as the process; nothing leaves Andon.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"andon/internal/drivers/services"
)

// demoHomelable holds the canvases, nodes and edges of one demo connection.
type demoHomelable struct {
	mu    sync.Mutex
	seq   int
	items map[string]map[string]map[string]any // "designs", "nodes", "edges" → id → item
}

var (
	demoHomelablesMu sync.Mutex
	demoHomelables   = map[string]*demoHomelable{}
)

// HomelableDemo is the demo instance of key (a connection).
func HomelableDemo(key string) HomelableAPI {
	demoHomelablesMu.Lock()
	defer demoHomelablesMu.Unlock()
	h, ok := demoHomelables[key]
	if !ok {
		h = &demoHomelable{items: map[string]map[string]map[string]any{"designs": {}, "nodes": {}, "edges": {}}}
		demoHomelables[key] = h
	}
	return h
}

func (h *demoHomelable) Get(ctx context.Context, path string) (any, error) {
	return h.Send(ctx, http.MethodGet, path, nil)
}

// Send answers like Homelable: list and create on a collection, change
// and delete on one item.
func (h *demoHomelable) Send(_ context.Context, method, path string, body any) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	kind, id, _ := strings.Cut(path, "/")
	items, ok := h.items[kind]
	if !ok {
		return nil, services.HomelableErr(http.StatusNotFound)
	}
	fields, _ := body.(map[string]any)

	switch {
	case method == http.MethodGet && id == "":
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out, nil
	case method == http.MethodPost && id == "":
		h.seq++
		item := map[string]any{"id": "demo-" + strconv.Itoa(h.seq)}
		for k, v := range fields {
			item[k] = v
		}
		items[item["id"].(string)] = item
		return item, nil
	}

	item, ok := items[id]
	if !ok {
		return nil, services.HomelableErr(http.StatusNotFound)
	}
	switch method {
	case http.MethodPatch:
		for k, v := range fields {
			item[k] = v
		}
		return item, nil
	case http.MethodDelete:
		delete(items, id)
		return nil, nil
	}
	return item, nil
}
