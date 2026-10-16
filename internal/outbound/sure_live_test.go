package outbound_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// kindTransaction is a Sure test booking in the write log.
const kindTransaction = "transaction"

// A booking of its own gets a category, as the cleanup bench files
// uncategorized bookings (services/categorize); deleted at the end.
// Sure's bookings are real bank data, so this runs against a throwaway
// instance only (live.Target).
func TestSureLive(t *testing.T) {
	to := live.Target(t, live.Sure)
	api := services.SureApi{URL: to.URL, Key: to.Token, Verify: to.VerifyTLS}
	ctx := context.Background()

	account := sureFirst(t, api, "accounts")
	category := sureFirst(t, api, "categories")
	created, err := api.Send(ctx, http.MethodPost, "transactions", map[string]any{"transaction": map[string]any{
		"account_id": account, "date": time.Now().Format(time.DateOnly), "amount": 1.23, "name": live.Name(t), "nature": "expense"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := asObject(created)["id"].(string)
	live.Created(t, live.Sure, kindTransaction, id)
	t.Cleanup(func() {
		live.Change(t, live.Sure, live.Delete, kindTransaction, id)
		if _, err := api.Send(ctx, http.MethodDelete, "transactions/"+id, nil); err != nil {
			t.Errorf("delete: %v", err)
		}
	})

	live.Change(t, live.Sure, live.Update, kindTransaction, id)
	if err := outbound.SureCategorySet(ctx, to, id, category); err != nil {
		t.Fatalf("category: %v", err)
	}
	got, err := api.Get(ctx, "transactions/"+id, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if c := asObject(asObject(got)["category"])["id"]; c != category {
		t.Errorf("category = %v, want %s", c, category)
	}
}

// sureFirst is the id of the first entry of a list ("accounts").
func sureFirst(t *testing.T, api services.SureApi, list string) string {
	t.Helper()
	raw, err := api.Get(context.Background(), list, nil)
	if err != nil {
		t.Fatalf("%s: %v", list, err)
	}
	entries, _ := asObject(raw)[list].([]any)
	if len(entries) == 0 {
		t.Skipf("no %s in Sure", list)
	}
	return asObject(entries[0])["id"].(string)
}
