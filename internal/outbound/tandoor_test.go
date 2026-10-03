package outbound_test

import (
	"context"
	"net/http"
	"testing"

	"andon/internal/outbound"
)

// TestTandoorCheck: one PATCH with the bearer token sets checked.
func TestTandoorCheck(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"id": 7, "checked": true}`))
	if err := outbound.TandoorCheck(context.Background(), to, 7); err != nil {
		t.Fatal(err)
	}
	c := f.one(t)
	if c.Method != http.MethodPatch || c.Path != "/api/shopping-list-entry/7/" || c.Header.Get("Authorization") != "Bearer "+token {
		t.Fatalf("call %s %s %q", c.Method, c.Path, c.Header.Get("Authorization"))
	}
	if c.json(t)["checked"] != true {
		t.Fatalf("body %s", c.Body)
	}
}

// TestGrocyAddMissing: one POST with Grocy's key header.
func TestGrocyAddMissing(t *testing.T) {
	var f fake
	to := f.serve(t, func(w http.ResponseWriter, _ call) { w.WriteHeader(http.StatusNoContent) })
	if err := outbound.GrocyAddMissing(context.Background(), to); err != nil {
		t.Fatal(err)
	}
	c := f.one(t)
	if c.Method != http.MethodPost || c.Path != "/api/stock/shoppinglist/add-missing-products" || c.Header.Get("GROCY-API-KEY") != token {
		t.Fatalf("call %s %s", c.Method, c.Path)
	}
}
