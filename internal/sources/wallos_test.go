package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// Trimmed answers of Wallos' API (api/subscriptions, api/currencies).
const (
	wallosSubs = `{"success": true, "title": "subscriptions", "subscriptions": [
		{"id": 1, "name": "Hetzner", "price": 38.2, "currency_id": 1, "next_payment": "2026-10-01", "cycle": 3, "frequency": 1,
		 "inactive": 0, "category_name": "Hosting", "payment_method_name": "Lastschrift", "url": "https://hetzner.com"},
		{"id": 2, "name": "Domain", "price": 24, "currency_id": 1, "next_payment": "2027-03-01", "cycle": 4, "frequency": 1, "inactive": 0},
		{"id": 3, "name": "Altes Abo", "price": 5, "currency_id": 1, "next_payment": "2026-01-01", "cycle": 3, "frequency": 1, "inactive": 1}
	], "notes": []}`
	wallosCurrencies = `{"success": true, "title": "currencies", "main_currency": 1,
		"currencies": [{"id": 1, "name": "Euro", "symbol": "€", "code": "EUR", "rate": "1", "in_use": true}], "notes": []}`
)

// TestWallosFetch: subscriptions with their monthly price (Wallos' own
// formula), the main currency, inactive ones marked; the key goes as
// api_key.
func TestWallosFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "key" {
			http.Error(w, `{"success": false}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/subscriptions/get_subscriptions.php":
			if r.URL.Query().Get("convert_currency") != "true" {
				t.Errorf("prices not converted: %s", r.URL.RawQuery)
			}
			w.Write([]byte(wallosSubs))
		case "/api/currencies/get_currencies.php":
			w.Write([]byte(wallosCurrencies))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := sources.WallosData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "key", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.WallosDataset)
	if d.Currency != "EUR" || len(d.Subs) != 3 {
		t.Fatalf("dataset: %+v", d)
	}
	if s := d.Subs[0]; s.Name != "Hetzner" || s.Monthly != 38.2 || s.Next != "2026-10-01" || s.Category != "Hosting" || s.Inactive {
		t.Fatalf("monthly: %+v", s)
	}
	if s := d.Subs[1]; s.Monthly != 2 {
		t.Fatalf("yearly per month: %+v", s)
	}
	if !d.Subs[2].Inactive {
		t.Fatalf("inactive: %+v", d.Subs[2])
	}
}
