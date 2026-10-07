package services

// Homelable, the homelab diagram tool (github.com/Pouzor/homelable),
// API read at v3.6.0 (commit 56d113d, backend/app/api/routes):
//
//	POST   /api/v1/auth/login   {"username", "password"} → {"access_token"}
//	GET    /api/v1/designs      canvases          POST /api/v1/designs
//	GET    /api/v1/nodes        all canvases      POST /api/v1/nodes
//	PATCH  /api/v1/nodes/{id}                     DELETE /api/v1/nodes/{id}
//	GET    /api/v1/edges        all canvases      POST /api/v1/edges
//	DELETE /api/v1/edges/{id}
//
// Every call but the login carries the JWT as a bearer token. It expires
// after 24 hours; the token is kept in memory per address and user, and
// a 401 signs in once more and repeats the call.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"andon/internal/drivers/httpclient"
)

// HomelableApi signs in with a local Homelable user.
type HomelableApi struct {
	URL, User, Password string
	Verify              bool
}

// homelableDetailMax caps the server's explanation kept in an error.
const homelableDetailMax = 200

var (
	homelableMu     sync.Mutex
	homelableTokens = map[string]string{} // by address and user, never stored
)

func (a HomelableApi) tokenKey() string { return strings.TrimRight(a.URL, "/") + "\x00" + a.User }

func (a HomelableApi) endpoint(path string) string {
	return strings.TrimRight(a.URL, "/") + "/api/v1/" + path
}

// Login signs in and remembers the token for the calls that follow.
func (a HomelableApi) Login(ctx context.Context) error {
	body := map[string]string{"username": a.User, "password": a.Password}
	out, _, err := a.call(ctx, http.MethodPost, "auth/login", "", body)
	if err != nil {
		return err
	}
	token, _ := asMap(out)["access_token"].(string)
	if token == "" {
		return ApiError{"login: no token"}
	}
	homelableMu.Lock()
	homelableTokens[a.tokenKey()] = token
	homelableMu.Unlock()
	return nil
}

// Get reads /api/v1/<path>, e.g. "designs".
func (a HomelableApi) Get(ctx context.Context, path string) (any, error) {
	return a.Send(ctx, http.MethodGet, path, nil)
}

// Send runs one call against /api/v1/<path>, signing in first when no
// token is known and once more when the token has expired (401).
func (a HomelableApi) Send(ctx context.Context, method, path string, body any) (any, error) {
	homelableMu.Lock()
	token := homelableTokens[a.tokenKey()]
	homelableMu.Unlock()

	if token != "" {
		out, status, err := a.call(ctx, method, path, token, body)
		if status != http.StatusUnauthorized {
			return out, err
		}
	}
	if err := a.Login(ctx); err != nil {
		return nil, err
	}

	homelableMu.Lock()
	token = homelableTokens[a.tokenKey()]
	homelableMu.Unlock()
	out, _, err := a.call(ctx, method, path, token, body)
	return out, err
}

// call runs one request and decodes its JSON answer; status is the HTTP
// status, 0 when no answer came.
func (a HomelableApi) call(ctx context.Context, method, path, token string, body any) (any, int, error) {
	headers := map[string]string{"Accept": "application/json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, 0, err
		}
		headers["Content-Type"] = "application/json"
	}

	resp, err := httpclient.Request(ctx, method, a.endpoint(path), httpclient.Options{Headers: headers, Body: raw, SkipVerify: !a.Verify})
	if err != nil {
		return nil, 0, ApiError{err.Error()}
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	if err != nil {
		return nil, resp.StatusCode, ApiError{"read failed"}
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, resp.StatusCode, ApiError{homelableError(resp.StatusCode, answer)}
	}
	if len(bytes.TrimSpace(answer)) == 0 {
		return nil, resp.StatusCode, nil // 204 No Content
	}
	var out any
	if err := json.Unmarshal(answer, &out); err != nil {
		return nil, resp.StatusCode, ApiError{"invalid JSON"}
	}
	return out, resp.StatusCode, nil
}

// homelableError is "HTTP 409: <detail>" with FastAPI's detail, if any.
func homelableError(status int, answer []byte) string {
	msg := fmt.Sprintf("HTTP %d", status)
	var problem struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(answer, &problem) != nil || problem.Detail == nil {
		return msg
	}
	detail, ok := problem.Detail.(string)
	if !ok {
		text, _ := json.Marshal(problem.Detail)
		detail = string(text)
	}
	if len(detail) > homelableDetailMax {
		detail = detail[:homelableDetailMax]
	}
	return msg + ": " + detail
}

// HomelableStatus reads the HTTP status of a failed call: 404 for an
// entity gone meanwhile; 0 if err is not an HTTP answer.
func HomelableStatus(err error) int {
	var apiErr ApiError
	if !errors.As(err, &apiErr) {
		return 0
	}
	var status int
	if _, scanErr := fmt.Sscanf(apiErr.msg, "HTTP %d", &status); scanErr != nil {
		return 0
	}
	return status
}

// HomelableErr is the error of a call Homelable answered with status,
// for stand-ins of the API (the demo's canvas store).
func HomelableErr(status int) error { return ApiError{fmt.Sprintf("HTTP %d", status)} }
