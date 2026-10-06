package outbound_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of Tandoor test entries in the write log.
const (
	kindEntry = "shopping-list-entry"
	kindFood  = "food"
)

// A shopping list entry for a food of its own, checked off, then both
// deleted.
func TestTandoorLive(t *testing.T) {
	tandoor := live.Target(t, live.Tandoor)
	api := services.BearerApi(tandoor.URL, tandoor.Token, httpclient.TLSOf(tandoor.VerifyTLS))
	ctx := context.Background()
	name := live.Name(t)

	raw, err := api.Post(ctx, "api/shopping-list-entry/", map[string]any{"food": map[string]any{"name": name}, "amount": 1})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	entry := asObject(raw)
	id := num(entry["id"])
	food := num(asObject(entry["food"])["id"])
	if id == 0 || food == 0 {
		t.Fatalf("create entry: no ids in %v", entry)
	}
	live.Created(t, live.Tandoor, kindFood, food)
	t.Cleanup(func() { tandoorDelete(t, tandoor, kindFood, food) })
	live.Created(t, live.Tandoor, kindEntry, id)
	t.Cleanup(func() { tandoorDelete(t, tandoor, kindEntry, id) })

	live.Change(t, live.Tandoor, live.Update, kindEntry, id)
	if err := outbound.TandoorCheck(ctx, tandoor, id); err != nil {
		t.Fatalf("check: %v", err)
	}
	got, err := api.Get(ctx, "api/shopping-list-entry/"+strconv.FormatInt(id, 10)+"/", nil)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if c := asObject(got)["checked"]; c != true {
		t.Errorf("checked = %v, want true", c)
	}
}

// tandoorDelete removes a test entry; Andon itself never deletes there.
func tandoorDelete(t *testing.T, to outbound.Target, kind string, id int64) {
	live.Change(t, live.Tandoor, live.Delete, kind, id)
	resp, err := httpclient.Request(context.Background(), http.MethodDelete, to.URL+"/api/"+kind+"/"+strconv.FormatInt(id, 10)+"/",
		httpclient.Options{Headers: map[string]string{"Authorization": "Bearer " + to.Token}, SkipVerify: !to.VerifyTLS})
	if err != nil {
		t.Errorf("delete %s %d: %v", kind, id, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		t.Errorf("delete %s %d: %s %s", kind, id, resp.Status, body)
	}
}
