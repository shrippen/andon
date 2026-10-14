package outbound

// Writes of the subscription bench: Invoice Ninja's recurring expenses
// and Wallos' subscriptions.

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"andon/internal/drivers/services"
)

// NinjaRecurringNew is a recurring expense to create; Frequency is its
// frequency_id (sources.NinjaFrequency).
type NinjaRecurringNew struct {
	VendorKey, CategoryKey, Notes string
	Amount                        float64
	Frequency                     int
	Next                          string // first booking, "2026-11-01"
	TaxName                       string
	TaxRate                       float64
}

// RecurringAction is what a bulk call does to a recurring expense.
type RecurringAction string

const (
	RecurringStart   RecurringAction = "start"
	RecurringStop    RecurringAction = "stop"
	RecurringArchive RecurringAction = "archive"
)

// recurringEntity is Invoice Ninja's path of recurring expenses.
const recurringEntity = "recurring_expenses"

// NinjaRecurringCreate creates a recurring expense, starts it and
// returns its key and number. A created but not started one is reported
// with its key: it exists, as a draft.
func NinjaRecurringCreate(ctx context.Context, to Target, r NinjaRecurringNew) (string, string, error) {
	body := map[string]any{"vendor_id": r.VendorKey, "amount": r.Amount, "frequency_id": strconv.Itoa(r.Frequency),
		"next_send_date": r.Next, "remaining_cycles": -1, "public_notes": r.Notes, "uses_inclusive_taxes": true}
	if r.CategoryKey != "" {
		body["category_id"] = r.CategoryKey
	}
	if r.TaxName != "" {
		body["tax_name1"], body["tax_rate1"] = r.TaxName, r.TaxRate
	}
	res, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, recurringEntity, body)
	if err != nil {
		return "", "", err
	}
	top, _ := res.(map[string]any)
	data, _ := top["data"].(map[string]any)
	key, _ := data["id"].(string)
	number, _ := data["number"].(string)
	if key == "" {
		return "", "", errors.New("invoice ninja: no recurring expense id in the answer")
	}
	return key, number, NinjaRecurringBulk(ctx, to, key, RecurringStart)
}

// NinjaRecurringAmount sets a recurring expense's amount; its next
// bookings take it.
func NinjaRecurringAmount(ctx context.Context, to Target, key string, amount float64) error {
	_, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Put(ctx, recurringEntity, key, map[string]any{"amount": amount})
	return err
}

// NinjaRecurringBulk starts, stops or archives one recurring expense.
func NinjaRecurringBulk(ctx context.Context, to Target, key string, action RecurringAction) error {
	_, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Post(ctx, recurringEntity+"/bulk",
		map[string]any{"action": string(action), "ids": []string{key}})
	return err
}

// WallosNew is a subscription to add; Cycle and Frequency as
// sources.WallosCycle gives them.
type WallosNew struct {
	Name             string
	Price            float64
	CurrencyID       int64
	Cycle, Frequency int
	Next             string // next payment, "2026-11-01"
}

// wallosWrite is Wallos' write script.
const wallosWrite = "api/subscriptions/set_subscriptions.php"

func wallos(to Target) services.WallosApi {
	return services.WallosApi{URL: to.URL, Key: to.Token, Verify: to.VerifyTLS}
}

func amountOf(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// WallosAdd adds a subscription and returns its id.
func WallosAdd(ctx context.Context, to Target, s WallosNew) (int64, error) {
	res, err := wallos(to).Post(ctx, wallosWrite, url.Values{"action": {"add"}, "name": {s.Name}, "price": {amountOf(s.Price)},
		"currency_id": {strconv.FormatInt(s.CurrencyID, 10)}, "cycle": {strconv.Itoa(s.Cycle)}, "frequency": {strconv.Itoa(s.Frequency)},
		"next_payment": {s.Next}, "auto_renew": {"1"}})
	if err != nil {
		return 0, err
	}
	top, _ := res.(map[string]any)
	id, _ := top["subscriptionId"].(float64)
	return int64(id), nil
}

// WallosPrice sets a subscription's price, in its own currency.
func WallosPrice(ctx context.Context, to Target, id int64, price float64) error {
	_, err := wallos(to).Post(ctx, wallosWrite, url.Values{"action": {"edit"}, "id": {strconv.FormatInt(id, 10)}, "price": {amountOf(price)}})
	return err
}

// WallosEnd marks a subscription as cancelled on day.
func WallosEnd(ctx context.Context, to Target, id int64, day string) error {
	_, err := wallos(to).Post(ctx, wallosWrite, url.Values{"action": {"edit"}, "id": {strconv.FormatInt(id, 10)},
		"inactive": {"1"}, "cancellation_date": {day}})
	return err
}

// WallosDelete removes a subscription, e.g. one entered twice.
func WallosDelete(ctx context.Context, to Target, id int64) error {
	_, err := wallos(to).Post(ctx, wallosWrite, url.Values{"action": {"delete"}, "id": {strconv.FormatInt(id, 10)}})
	return err
}
