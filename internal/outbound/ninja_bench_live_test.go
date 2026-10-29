package outbound_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of Invoice Ninja test entries in the write log.
const (
	kindBank     = "bank_integrations"
	kindBankTxn  = "bank_transactions"
	kindVendor   = "vendors"
	ninjaMatched = "3" // status_id of a booked transaction
	ninjaHistory = "activities/entity"
	reminderOne  = 63.0 // activity of a sent first reminder
)

// A bank account of its own with two debits: one booked against an
// expense, one archived and restored, as the VAT check does them.
func TestNinjaBankLive(t *testing.T) {
	ninja := live.Target(t, live.Ninja)
	api := services.NinjaApi{URL: ninja.URL, Token: ninja.Token, Verify: ninja.VerifyTLS}
	ctx := context.Background()
	name := live.Name(t)
	today := time.Now().Format(time.DateOnly)

	bank := ninjaKey(ninjaPost(t, api, kindBank, map[string]any{"bank_account_name": name}))
	ninjaOwn(t, api, kindBank, bank)
	txn := func() string {
		key := ninjaKey(ninjaPost(t, api, kindBankTxn, map[string]any{"bank_integration_id": bank, "amount": 1.23,
			"base_type": "DEBIT", "date": today, "description": name}))
		ninjaOwn(t, api, kindBankTxn, key)
		return key
	}
	booked, archived := txn(), txn()

	expense, _, err := outbound.NinjaExpenseCreate(ctx, ninja, map[string]any{"amount": 1.23, "date": today, "public_notes": name})
	if err != nil {
		t.Fatalf("expense: %v", err)
	}
	ninjaOwn(t, api, kindExpense, expense)

	live.Change(t, live.Ninja, live.Update, kindBankTxn, booked)
	if err := outbound.NinjaBankLink(ctx, ninja, booked, outbound.NinjaBankTarget{Expenses: []string{expense}}); err != nil {
		t.Fatalf("link: %v", err)
	}
	if got := ninjaRead(t, api, kindBankTxn, booked); got["status_id"] != ninjaMatched || got["expense_id"] != expense {
		t.Errorf("booked: status %v, expense %v", got["status_id"], got["expense_id"])
	}

	for _, action := range []outbound.BankArchive{outbound.BankArchiveOn, outbound.BankArchiveOff} {
		live.Change(t, live.Ninja, live.Update, kindBankTxn, archived)
		if err := outbound.NinjaBankArchive(ctx, ninja, archived, action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if at := ninjaRead(t, api, kindBankTxn, archived)["archived_at"]; at != 0.0 {
		t.Errorf("restored transaction still archived: %v", at)
	}
}

// A vendor created and archived, as the VAT check and the vendor
// cleanup do it.
func TestNinjaVendorLive(t *testing.T) {
	ninja := live.Target(t, live.Ninja)
	api := services.NinjaApi{URL: ninja.URL, Token: ninja.Token, Verify: ninja.VerifyTLS}
	ctx := context.Background()

	vendor, err := outbound.NinjaVendorCreate(ctx, ninja, live.Name(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ninjaOwn(t, api, kindVendor, vendor)

	live.Change(t, live.Ninja, live.Update, kindVendor, vendor)
	if err := outbound.NinjaVendorArchive(ctx, ninja, vendor); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if at, _ := ninjaRead(t, api, kindVendor, vendor)["archived_at"].(float64); at == 0 {
		t.Error("vendor not archived")
	}
}

// A first reminder for an invoice of its own client, as the dunning run
// sends it; Invoice Ninja records it in the invoice's history only.
func TestNinjaReminderLive(t *testing.T) {
	ninja := live.Target(t, live.Ninja)
	api := services.NinjaApi{URL: ninja.URL, Token: ninja.Token, Verify: ninja.VerifyTLS}
	ctx := context.Background()
	name := live.Name(t)

	client := ninjaKey(ninjaPost(t, api, kindClient, map[string]any{"name": name,
		"contacts": []map[string]any{{"email": "andon-test@example.test", "send_email": true}}}))
	ninjaOwn(t, api, kindClient, client)
	number, err := outbound.NinjaDraftInvoice(ctx, ninja, client, []outbound.NinjaLine{{Product: "andon-test", Notes: name, Quantity: 1, Cost: 1}})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	invoice := ninjaFind(t, api, kindInvoice, client, "number", number)
	ninjaOwn(t, api, kindInvoice, invoice)

	live.Change(t, live.Ninja, live.Update, kindInvoice, invoice)
	if err := outbound.NinjaReminder(ctx, ninja, invoice, 1, "andon-test", name); err != nil {
		t.Fatalf("reminder: %v", err)
	}
	history := ninjaPost(t, api, ninjaHistory, map[string]any{"entity": "invoice", "entity_id": invoice})
	for _, a := range rows(history) {
		if a["activity_type_id"] == reminderOne {
			return
		}
	}
	t.Errorf("no reminder in the history: %v", history)
}

// ninjaPost sends one create call and fails the test on an error.
func ninjaPost(t *testing.T, api services.NinjaApi, path string, body any) any {
	t.Helper()
	out, err := api.Post(context.Background(), path, body)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	return out
}
