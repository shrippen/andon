package outbound_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of Invoice Ninja test entries in the write log; each is also the
// entity's path in the API.
const (
	kindClient  = "clients"
	kindInvoice = "invoices"
	kindPayment = "payments"
	kindExpense = "expenses"
)

// ninjaDelete is the bulk action that deletes entries.
const ninjaDelete = "delete"

// ninjaPaid is the status_id of a paid invoice.
const ninjaPaid = "4"

// A client of its own gets an invoice draft, a payment of it and an
// expense with a custom value; all are deleted at the end. Invoice
// Ninja hands out invoice and expense numbers on save, so each run
// leaves gaps in both counters: it runs only when asked for
// (live.Target).
func TestNinjaLive(t *testing.T) {
	ninja := live.Target(t, live.Ninja)
	api := services.NinjaApi{URL: ninja.URL, Token: ninja.Token, Verify: ninja.VerifyTLS}
	ctx := context.Background()
	name := live.Name(t)
	today := time.Now().Format(time.DateOnly)

	created, err := api.Post(ctx, kindClient, map[string]any{"name": name})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	client := ninjaKey(created)
	ninjaOwn(t, api, kindClient, client)

	number, err := outbound.NinjaDraftInvoice(ctx, ninja, client, []outbound.NinjaLine{{Product: "andon-test", Notes: name, Quantity: 1, Cost: 1}})
	if err != nil {
		t.Fatalf("draft invoice: %v", err)
	}
	invoice := ninjaFind(t, api, kindInvoice, client, "number", number)
	ninjaOwn(t, api, kindInvoice, invoice)

	// The whole amount, taxes included, so the invoice ends up paid.
	amount, _ := ninjaRead(t, api, kindInvoice, invoice)["amount"].(float64)
	live.Change(t, live.Ninja, live.Update, kindInvoice, invoice)
	if err := outbound.NinjaPayment(ctx, ninja, client, invoice, amount, today, name); err != nil {
		t.Fatalf("payment: %v", err)
	}
	payment := ninjaFind(t, api, kindPayment, client, "transaction_reference", name)
	ninjaOwn(t, api, kindPayment, payment)
	if s := ninjaRead(t, api, kindInvoice, invoice)["status_id"]; s != ninjaPaid {
		t.Errorf("invoice status after payment = %v, want %s", s, ninjaPaid)
	}

	expense, _, err := outbound.NinjaExpenseCreate(ctx, ninja, map[string]any{"amount": 1, "date": today, "client_id": client, "public_notes": name})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	ninjaOwn(t, api, kindExpense, expense)

	live.Change(t, live.Ninja, live.Update, kindExpense, expense)
	if err := outbound.NinjaExpenseSet(ctx, ninja, expense, map[string]string{"custom_value1": name}); err != nil {
		t.Fatalf("set expense: %v", err)
	}
	if v := ninjaRead(t, api, kindExpense, expense)["custom_value1"]; v != name {
		t.Errorf("custom_value1 = %v, want %q", v, name)
	}
}

// ninjaOwn logs a created entry and deletes it when the test ends
// (cleanups run last-created first: payment before invoice).
func ninjaOwn(t *testing.T, api services.NinjaApi, kind, key string) {
	t.Helper()
	if key == "" {
		t.Fatalf("no %s id in the answer", kind)
	}
	live.Created(t, live.Ninja, kind, key)
	t.Cleanup(func() {
		live.Change(t, live.Ninja, live.Delete, kind, key)
		if _, err := api.Post(context.Background(), kind+"/bulk", map[string]any{"action": ninjaDelete, "ids": []string{key}}); err != nil {
			t.Errorf("delete %s %s: %v", kind, key, err)
		}
	})
}

// ninjaFind returns the key of the client's entry whose field is want;
// the write calls answer without it.
func ninjaFind(t *testing.T, api services.NinjaApi, kind, client, field, want string) string {
	t.Helper()
	raw, err := api.Get(context.Background(), kind, url.Values{"client_id": {client}})
	if err != nil {
		t.Fatalf("list %s: %v", kind, err)
	}
	for _, e := range rows(raw) {
		if e[field] == want {
			key, _ := e["id"].(string)
			return key
		}
	}
	t.Fatalf("no %s with %s %q", kind, field, want)
	return ""
}

func ninjaRead(t *testing.T, api services.NinjaApi, kind, key string) map[string]any {
	t.Helper()
	raw, err := api.Get(context.Background(), kind+"/"+key, nil)
	if err != nil {
		t.Fatalf("read %s %s: %v", kind, key, err)
	}
	m, _ := raw.(map[string]any)
	data, _ := m["data"].(map[string]any)
	return data
}

// ninjaKey reads the hashed id of a create answer.
func ninjaKey(raw any) string {
	m, _ := raw.(map[string]any)
	data, _ := m["data"].(map[string]any)
	key, _ := data["id"].(string)
	return key
}
