package outbound_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/outbound"
)

// A new thing goes to the room named, else the location's "Unplaced"; a
// room not there is refused. A warranty goes through Kept's MCP tool, whose refusal comes back as an error.
func TestKeptWrites(t *testing.T) {
	var created, tool map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kpt_w" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/locations/loc1/places":
			io.WriteString(w, `{"places":[{"id":"room","name":"Werkstatt","isUnplaced":false},{"id":"unplaced","name":"Unplaced","isUnplaced":true}]}`)
		case "POST /api/v1/things":
			json.Unmarshal(body, &created)
			io.WriteString(w, `{"id":"t9","shortCode":"ABCDEF"}`)
		case "POST /mcp":
			var rpc struct{ Params map[string]any }
			json.Unmarshal(body, &rpc)
			tool = rpc.Params
			args, _ := tool["arguments"].(map[string]any)
			if args["thing_id"] == "gone" {
				io.WriteString(w, "event: message\ndata: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"error\\\":\\\"not_found\\\",\\\"hint\\\":\\\"Not found.\\\"}\"}],\"isError\":true}}\n\n")
				return
			}
			io.WriteString(w, "event: message\ndata: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"data\\\":{\\\"warranty_id\\\":\\\"w1\\\",\\\"ends_on\\\":\\\"2028-03-14\\\"}}\"}]}}\n\n")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	to := outbound.Target{URL: srv.URL, Token: "kpt_w"}
	ctx := context.Background()

	id, err := outbound.KeptThingCreate(ctx, to, outbound.KeptNew{Location: "loc1", Name: "NAS", Notes: "192.168.1.5"})
	if err != nil || id != "t9" {
		t.Fatalf("create %q %v", id, err)
	}
	if created["placeId"] != "unplaced" || created["name"] != "NAS" || created["notes"] != "192.168.1.5" || created["serial"] != nil {
		t.Errorf("body %v", created)
	}

	if _, err := outbound.KeptThingCreate(ctx, to, outbound.KeptNew{Location: "loc1", Place: " werkstatt ", Name: "NAS"}); err != nil || created["placeId"] != "room" {
		t.Errorf("room: %v %v", err, created)
	}
	if _, err := outbound.KeptThingCreate(ctx, to, outbound.KeptNew{Location: "loc1", Place: "Keller", Name: "NAS"}); !errors.Is(err, outbound.ErrKeptNoPlace) {
		t.Errorf("no room: %v", err)
	}

	if err := outbound.KeptWarranty(ctx, to, "t9", 24); err != nil {
		t.Fatal(err)
	}
	args, _ := tool["arguments"].(map[string]any)
	if tool["name"] != "add_warranty" || args["thing_id"] != "t9" || args["term_months"] != 24.0 || args["kind"] != "manufacturer" {
		t.Errorf("tool %v", tool)
	}
	if err := outbound.KeptWarranty(ctx, to, "gone", 24); err == nil || err.Error() != "not_found: Not found." {
		t.Errorf("refusal: %v", err)
	}
}
