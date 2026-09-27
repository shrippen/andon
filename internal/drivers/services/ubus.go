package services

import (
	"context"
	"fmt"

	"andon/internal/drivers/httpclient"
)

// UbusApi talks to OpenWrt's rpcd over HTTP (uhttpd-mod-ubus, /ubus):
// a login returns a session, every call is
//
//	{"method": "call", "params": [session, object, method, args]}
//	→ {"result": [0, {...}]}   0 = ok, 6 = permission denied
type UbusApi struct {
	URL    string
	Verify bool
}

// ubusNoSession is rpcd's session id before login.
const ubusNoSession = "00000000000000000000000000000000"

// Login opens a session for user and password.
func (a UbusApi) Login(ctx context.Context, user, password string) (string, error) {
	out, err := a.Call(ctx, ubusNoSession, "session", "login", map[string]any{"username": user, "password": password})
	if err != nil {
		return "", err
	}
	session, _ := asMap(out)["ubus_rpc_session"].(string)
	if session == "" {
		return "", ApiError{"ubus: no session"}
	}
	return session, nil
}

// Call runs object.method with args in a session and returns its data.
func (a UbusApi) Call(ctx context.Context, session, object, method string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "call", "params": []any{session, object, method, args}}
	out, err := postJSON(ctx, joinURL(a.URL, "ubus"), nil, body, httpclient.TLSOf(a.Verify))
	if err != nil {
		return nil, err
	}
	resp := asMap(out)
	if e := asMap(resp["error"]); len(e) > 0 {
		return nil, ApiError{fmt.Sprintf("ubus %s.%s: %v", object, method, e["message"])}
	}
	result, _ := resp["result"].([]any)
	if len(result) == 0 {
		return nil, ApiError{fmt.Sprintf("ubus %s.%s: empty answer", object, method)}
	}
	if code, _ := result[0].(float64); code != 0 {
		return nil, ApiError{fmt.Sprintf("ubus %s.%s: status %v", object, method, code)}
	}
	if len(result) < 2 {
		return map[string]any{}, nil
	}
	return result[1], nil
}
