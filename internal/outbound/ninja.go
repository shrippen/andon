package outbound

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"andon/internal/drivers/services"
)

// NinjaLine is one invoice position to create.
type NinjaLine struct {
	Product, Notes string
	Quantity, Cost float64
}

// NinjaDraftInvoice creates an invoice draft for a client (hashed v5 id)
// and returns the new invoice's number.
func NinjaDraftInvoice(ctx context.Context, to Target, clientKey string, lines []NinjaLine) (string, error) {
	items := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]any{"product_key": l.Product, "notes": l.Notes, "quantity": l.Quantity, "cost": l.Cost})
	}
	body, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, "invoices",
		map[string]any{"client_id": clientKey, "line_items": items})
	if err != nil {
		return "", err
	}
	data, _ := body.(map[string]any)
	inner, _ := data["data"].(map[string]any)
	number, _ := inner["number"].(string)
	return number, nil
}

// NinjaPayment records a payment of one invoice (hashed v5 ids).
func NinjaPayment(ctx context.Context, to Target, clientKey, invoiceKey string, amount float64, day, reference string) error {
	_, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, "payments", map[string]any{
		"client_id": clientKey, "amount": amount, "date": day, "transaction_reference": reference,
		"invoices": []map[string]any{{"invoice_id": invoiceKey, "amount": amount}},
	})
	return err
}

// ErrNotBooked means Invoice Ninja took a match but did not book it
// (the expense already had a transaction, the invoice was paid).
var ErrNotBooked = errors.New("invoice ninja: transaction not booked")

// ninjaConverted is Invoice Ninja's status_id of a booked transaction.
const ninjaConverted = "3"

// NinjaBankLink books a bank transaction (hashed v5 id) against an
// existing expense, or against open invoices, which pays them.
func NinjaBankLink(ctx context.Context, to Target, txnKey string, link NinjaBankTarget) error {
	item := map[string]any{"id": txnKey}
	if link.Expense != "" {
		item["expense_id"] = link.Expense
	} else {
		item["invoice_ids"] = strings.Join(link.Invoices, ",")
	}
	body, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, "bank_transactions/match",
		map[string]any{"transactions": []map[string]any{item}})
	if err != nil {
		return err
	}

	// The answer lists the transactions it booked; a refused one is
	// missing or still open.
	top, _ := body.(map[string]any)
	list, _ := top["data"].([]any)
	for _, raw := range list {
		t, _ := raw.(map[string]any)
		if id, _ := t["id"].(string); id == txnKey && fmt.Sprint(t["status_id"]) == ninjaConverted {
			return nil
		}
	}
	return ErrNotBooked
}

// NinjaBankTarget is what a transaction is booked against: one expense,
// or invoices.
type NinjaBankTarget struct {
	Expense  string
	Invoices []string
}

// NinjaVendorCreate creates a vendor and returns its hashed id.
func NinjaVendorCreate(ctx context.Context, to Target, name string) (string, error) {
	body, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, "vendors", map[string]any{"name": name})
	if err != nil {
		return "", err
	}
	top, _ := body.(map[string]any)
	data, _ := top["data"].(map[string]any)
	key, _ := data["id"].(string)
	if key == "" {
		return "", errors.New("invoice ninja: no vendor id in the answer")
	}
	return key, nil
}
