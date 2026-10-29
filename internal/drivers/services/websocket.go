package services

// WebSocket plumbing shared by the clients that speak a request/answer
// protocol over one short-lived connection (TrueNAS JSON-RPC, Home
// Assistant):
//
//	wsDial  ── http(s)://host + path ──► ws(s)://host/path, read limit set
//	wsCall  ── {"id": n, …} ──► skips other messages ──► the answer with id n

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	"andon/internal/drivers/httpclient"
)

// wsDial opens a WebSocket to base + path; status is the HTTP status of
// a refused upgrade (0 when there was no answer).
func wsDial(ctx context.Context, base, path string, verify bool) (conn *websocket.Conn, status int, err error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/" + path)
	if err != nil {
		return nil, 0, ApiError{"bad url"}
	}
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	dialCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	conn, resp, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: httpclient.ClientTLS(rpcTimeout, httpclient.TLSOf(verify))})
	if err != nil {
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, status, ApiError{"connect failed"}
	}
	conn.SetReadLimit(rpcMaxRead)
	return conn, 0, nil
}

// wsCall sends req and returns the first message whose "id" is id;
// notifications and other answers in between are skipped.
func wsCall(ctx context.Context, conn *websocket.Conn, id int, req []byte) ([]byte, error) {
	if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
		return nil, ApiError{"write failed"}
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return nil, ApiError{"read failed"}
		}
		var msg struct {
			ID *int `json:"id"`
		}
		if json.Unmarshal(raw, &msg) == nil && msg.ID != nil && *msg.ID == id {
			return raw, nil
		}
	}
}
