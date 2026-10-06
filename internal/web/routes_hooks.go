package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"andon/internal/services/hooks"
)

// hookBodyMax bounds a body: an event is small, a Hansei state lists
// the findings it works on (a few hundred ids).
const hookBodyMax = 128 << 10

// RegisterHookRoutes wires inbound webhooks (no session: the URL carries
// its own signature).
func (d Deps) RegisterHookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /hooks/{id}/{sig}", d.handleHook)
}

// hookBody is what a PG Back Web webhook is configured to send, e.g.
// {"event": "execution_failed", "name": "kimai"} (query parameters of
// the same names work too), or a service's whole state, e.g. Hansei's
// {"state": {"review": 2, …}}.
type hookBody struct {
	Event string         `json:"event"`
	Name  string         `json:"name"`
	State map[string]any `json:"state"`
}

func (d Deps) handleHook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var body hookBody
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, hookBodyMax)).Decode(&body)
	if body.Event == "" {
		body.Event = r.URL.Query().Get("event")
	}
	if body.Name == "" {
		body.Name = r.URL.Query().Get("name")
	}

	receive := func() error { return hooks.Receive(d.DB, id, r.PathValue("sig"), body.Event, body.Name) }
	if body.State != nil {
		receive = func() error { return hooks.ReceiveState(d.DB, id, r.PathValue("sig"), body.State) }
	}

	if err := receive(); err != nil {
		if errors.Is(err, hooks.ErrRejected) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, hooks.ErrThrottled) {
			http.Error(w, "too many events", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
