package services

// Home Assistant's WebSocket API (/api/websocket): the only way to its
// statistics and energy settings. One short session per fetch:
//
//	server: {"type": "auth_required"}
//	client: {"type": "auth", "access_token": "…"}
//	server: {"type": "auth_ok"}            (or auth_invalid)
//	client: {"id": 1, "type": "energy/get_prefs"}
//	server: {"id": 1, "type": "result", "success": true, "result": {…}}

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
)

// Messages of the login handshake.
const (
	hassAuth     = "auth"
	hassAuthOK   = "auth_ok"
	hassAuthWant = "auth_required"
)

// HassSession is one logged-in WebSocket connection.
type HassSession struct {
	conn *websocket.Conn
	id   int
}

// Open connects and logs in with the connection's token.
func (a HassApi) Open(ctx context.Context) (*HassSession, error) {
	conn, _, err := wsDial(ctx, a.URL, "api/websocket", a.Verify)
	if err != nil {
		return nil, err
	}
	s := &HassSession{conn: conn}
	if err := s.login(ctx, a.Token); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// login answers the server's auth_required with the token.
func (s *HassSession) login(ctx context.Context, token string) error {
	callCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	var msg struct {
		Type string `json:"type"`
	}
	if err := s.read(callCtx, &msg); err != nil || msg.Type != hassAuthWant {
		return ApiError{"login failed"}
	}
	req, _ := json.Marshal(map[string]string{"type": hassAuth, "access_token": token})
	if err := s.conn.Write(callCtx, websocket.MessageText, req); err != nil {
		return ApiError{"write failed"}
	}
	if err := s.read(callCtx, &msg); err != nil || msg.Type != hassAuthOK {
		return ApiError{"login failed"}
	}
	return nil
}

func (s *HassSession) read(ctx context.Context, out any) error {
	_, raw, err := s.conn.Read(ctx)
	if err != nil {
		return ApiError{"read failed"}
	}
	return json.Unmarshal(raw, out)
}

// Close ends the session.
func (s *HassSession) Close() {
	s.conn.Close(websocket.StatusNormalClosure, "")
}

// Command runs one command, e.g. ("recorder/list_statistic_ids",
// {"statistic_type": "sum"}), and returns its result.
func (s *HassSession) Command(ctx context.Context, kind string, params map[string]any) (any, error) {
	s.id++
	body := map[string]any{"id": s.id, "type": kind}
	for k, v := range params {
		body[k] = v
	}
	req, _ := json.Marshal(body)
	callCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	raw, err := wsCall(callCtx, s.conn, s.id, req)
	if err != nil {
		return nil, err
	}
	var msg struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return nil, ApiError{kind + ": invalid result"}
	}
	if !msg.Success {
		reason := "failed"
		if msg.Error != nil {
			reason = msg.Error.Message
		}
		return nil, ApiError{kind + ": " + reason}
	}
	var out any
	if len(msg.Result) > 0 && json.Unmarshal(msg.Result, &out) != nil {
		return nil, ApiError{kind + ": invalid result"}
	}
	return out, nil
}
