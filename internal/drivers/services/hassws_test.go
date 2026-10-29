package services_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"

	"andon/internal/drivers/services"
)

// hassWS is a Home Assistant WebSocket: it wants the token "tok", sends
// an event before each answer and fails unknown commands.
func hassWS(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/websocket" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		send := func(v any) {
			raw, _ := json.Marshal(v)
			conn.Write(ctx, websocket.MessageText, raw)
		}

		send(map[string]any{"type": "auth_required"})
		var msg map[string]any
		if _, raw, err := conn.Read(ctx); err != nil || json.Unmarshal(raw, &msg) != nil || msg["access_token"] != "tok" {
			send(map[string]any{"type": "auth_invalid"})
			return
		}
		send(map[string]any{"type": "auth_ok"})
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var req struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			}
			json.Unmarshal(raw, &req)
			send(map[string]any{"id": 99, "type": "event"})
			if req.Type != "get_config" {
				send(map[string]any{"id": req.ID, "type": "result", "success": false, "error": map[string]any{"code": "unknown_command", "message": "Unknown command."}})
				continue
			}
			send(map[string]any{"id": req.ID, "type": "result", "success": true, "result": map[string]any{"time_zone": "Europe/Berlin"}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A session logs in, skips events, reads results and reports failures.
func TestHassSession(t *testing.T) {
	srv := hassWS(t)
	ctx := context.Background()

	s, err := services.HassApi{URL: srv.URL, Token: "tok"}.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	out, err := s.Command(ctx, "get_config", nil)
	if err != nil || out.(map[string]any)["time_zone"] != "Europe/Berlin" {
		t.Fatalf("get_config: %v %v", out, err)
	}
	if _, err := s.Command(ctx, "energy/get_prefs", nil); err == nil || err.Error() != "energy/get_prefs: Unknown command." {
		t.Fatalf("failure: %v", err)
	}

	if _, err := (services.HassApi{URL: srv.URL, Token: "wrong"}).Open(ctx); err == nil {
		t.Fatal("wrong token logged in")
	}
}
