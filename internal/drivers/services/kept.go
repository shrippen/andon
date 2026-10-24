package services

// Kept (home inventory): a personal token per person, read or read+write,
// reaching the locations it was made for. Kept hands tokens out over
// https only. Changes name the row version they start from (If-Match);
// what REST keeps from tokens (warranties) goes through Kept's MCP tools.
//
//	GET   api/v1/…              Authorization: Bearer kpt_…
//	PATCH api/v1/things/{id}    If-Match: <rowVersion>
//	POST  mcp                   JSON-RPC tools/call, answered as JSON or SSE

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"andon/internal/drivers/httpclient"
)

const (
	keptMCPPath = "mcp"
	keptPage    = 100
	keptMaxPage = 50 // pages of keptPage rows: 5000 things
	sseData     = "data:"
)

// KeptApi reads and writes one Kept instance.
type KeptApi struct {
	URL    string
	Token  string
	Verify bool
}

func (a KeptApi) keyed() KeyedApi {
	return BearerApi(a.URL, a.Token, httpclient.TLSOf(a.Verify))
}

// Get reads one endpoint.
func (a KeptApi) Get(ctx context.Context, path string, params url.Values) (any, error) {
	return a.keyed().Get(ctx, path, params)
}

// Items reads every page of a list ({items, next_cursor}).
func (a KeptApi) Items(ctx context.Context, path string, params url.Values) ([]any, error) {
	q := cloneValues(params)
	q.Set("limit", strconv.Itoa(keptPage))
	var out []any
	for range keptMaxPage {
		raw, err := a.Get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		page := asMap(raw)
		out = append(out, asList(page["items"])...)
		next, _ := page["next_cursor"].(string)
		if next == "" {
			return out, nil
		}
		q.Set("cursor", next)
	}
	return out, nil
}

// Send writes a JSON body; version, when set, is the row version the
// change starts from (Kept answers 412 when the row moved on).
func (a KeptApi) Send(ctx context.Context, method, path string, body any, version int) (any, error) {
	headers := a.keyed().Headers
	if version > 0 {
		headers = cloneHeaders(headers)
		headers["If-Match"] = strconv.Itoa(version)
	}
	return sendJSON(ctx, method, joinURL(a.URL, path), headers, body, httpclient.TLSOf(a.Verify))
}

// Tools lists the names of the MCP tools the token gets.
func (a KeptApi) Tools(ctx context.Context) ([]any, error) {
	raw, err := a.rpc(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, ApiError{"invalid MCP answer"}
	}
	out := make([]any, 0, len(list.Result.Tools))
	for _, t := range list.Result.Tools {
		out = append(out, t.Name)
	}
	return out, nil
}

// Tool calls one of Kept's MCP tools and returns its result's data; a
// refusal (isError) comes back as an ApiError with Kept's text.
func (a KeptApi) Tool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	raw, err := a.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	return toolResult(raw)
}

// rpc sends one JSON-RPC request to Kept's MCP door and returns the
// answer's JSON, unwrapped from server-sent events where Kept streams it.
func (a KeptApi) rpc(ctx context.Context, method string, params map[string]any) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	headers := cloneHeaders(a.keyed().Headers)
	headers["Content-Type"] = "application/json"
	headers["Accept"] = "application/json, text/event-stream"
	resp, err := httpclient.Request(ctx, http.MethodPost, joinURL(a.URL, keptMCPPath), httpclient.Options{Headers: headers, Body: body, SkipVerify: !a.Verify})
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, ApiError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	return unwrapSSE(raw), nil
}

// unwrapSSE returns the JSON of an answer, plain or as server-sent events:
//
//	event: message
//	data: {"result": …}
func unwrapSSE(raw []byte) []byte {
	payload := bytes.TrimSpace(raw)
	if bytes.HasPrefix(payload, []byte("{")) {
		return payload
	}
	lines := bufio.NewScanner(bytes.NewReader(raw))
	lines.Buffer(make([]byte, 0, len(raw)+1), len(raw)+1)
	for lines.Scan() {
		if line := lines.Text(); strings.HasPrefix(line, sseData) {
			payload = []byte(strings.TrimSpace(strings.TrimPrefix(line, sseData)))
		}
	}
	return payload
}

// toolResult reads a tools/call answer: its first text holds the tool's
// JSON envelope.
func toolResult(payload []byte) (map[string]any, error) {
	var rpc struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &rpc); err != nil {
		return nil, ApiError{"invalid MCP answer"}
	}
	if rpc.Error != nil {
		return nil, ApiError{rpc.Error.Message}
	}
	if len(rpc.Result.Content) == 0 {
		return nil, ApiError{"empty MCP answer"}
	}
	// The text is JSON: {"data": …} or a refusal {"error": "not_found", "hint": "Not found."}.
	var out struct {
		Data  map[string]any `json:"data"`
		Error string         `json:"error"`
		Hint  string         `json:"hint"`
	}
	text := rpc.Result.Content[0].Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		if rpc.Result.IsError {
			return nil, ApiError{text}
		}
		return nil, ApiError{"invalid MCP answer"}
	}
	if rpc.Result.IsError || out.Error != "" {
		return nil, ApiError{strings.TrimSpace(out.Error + ": " + out.Hint)}
	}
	return out.Data, nil
}

func cloneHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h)+2)
	for k, v := range h {
		out[k] = v
	}
	return out
}
