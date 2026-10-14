package verbund_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/connections"
	"andon/internal/services/verbund"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// accountFakes: Invoice Ninja's feed reads "Girokonto" (G1) and
// "Privat" (P1); Sure has "Geschäftskonto" (s-biz, two bookings the feed
// shares with G1), "Privat" (s-priv) and "Tagesgeld" (s-save).
func accountFakes(t *testing.T) (string, string) {
	t.Helper()
	day := time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly)
	page := func(items string) string {
		return `{"data": [` + items + `], "meta": {"pagination": {"total_pages": 1}}}`
	}
	ninja := http.NewServeMux()
	ninja.HandleFunc("GET /api/v1/{what}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("what") {
		case "bank_integrations":
			w.Write([]byte(page(`{"id": "G1", "bank_account_name": "Girokonto"}, {"id": "P1", "bank_account_name": "Privat"}`)))
		case "bank_transactions":
			w.Write([]byte(page(`{"id": "b1", "date": "` + day + `", "amount": 41.65, "base_type": "DEBIT", "status_id": "1", "bank_integration_id": "G1"},
				{"id": "b2", "date": "` + day + `", "amount": 899, "base_type": "DEBIT", "status_id": "1", "bank_integration_id": "G1"}`)))
		default:
			w.Write([]byte(page("")))
		}
	})
	sure := http.NewServeMux()
	for path, body := range map[string]string{
		"/api/v1/balance_sheet": `{"currency": "EUR"}`,
		"/api/v1/accounts":      `{"accounts": [{"id": "s-biz", "name": "Geschäftskonto"}, {"id": "s-priv", "name": "Privat"}, {"id": "s-save", "name": "Tagesgeld"}]}`,
		"/api/v1/transactions": `{"transactions": [{"id": "t1", "date": "` + day + `", "signed_amount_cents": -4165, "account": {"id": "s-biz", "name": "Geschäftskonto"}},
			{"id": "t2", "date": "` + day + `", "signed_amount_cents": -89900, "account": {"id": "s-biz", "name": "Geschäftskonto"}}]}`,
	} {
		sure.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
	n, s := httptest.NewServer(ninja), httptest.NewServer(sure)
	t.Cleanup(n.Close)
	t.Cleanup(s.Close)
	return n.URL, s.URL
}

// Accounts: shared bookings suggest Geschäftskonto for Girokonto, the
// name Privat for Privat; Tagesgeld stays loose. A role set on Girokonto
// reaches the linked Sure account once confirmed.
func TestAccounts(t *testing.T) {
	w := newWorld(t)
	nURL, sURL := accountFakes(t)
	ninja := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceInvoiceNinja, nURL)
	sure := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceSure, sURL)
	fetch(t, w, ninja, sure)
	id, err := verbund.Create(w.d, w.user, "Firma", []int64{ninja, sure}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	v, err := verbund.Accounts(ctx, w.d, w.user, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Hub.Service != enums.ServiceInvoiceNinja || len(v.Rows) != 2 || !v.CanSet {
		t.Fatalf("view: %+v", v)
	}
	cells := map[string]verbund.AccountCell{}
	for _, r := range v.Rows {
		cells[r.Key] = r.Cells[0]
	}
	if c := cells["G1"]; c.State != verbund.CustomerSuggested || c.Key != "s-biz" {
		t.Errorf("Girokonto: %+v", c)
	}
	if c := cells["P1"]; c.State != verbund.CustomerSuggested || c.Key != "s-priv" {
		t.Errorf("Privat: %+v", c)
	}
	if len(v.Loose) != 1 || v.Loose[0].Key != "s-save" {
		t.Errorf("loose: %+v", v.Loose)
	}

	if n, err := verbund.ConfirmAccounts(ctx, w.d, w.user, id, ""); err != nil || n != 2 {
		t.Fatalf("confirm: %d %v", n, err)
	}
	if err := verbund.SetRole(w.d, w.user, ninja, "G1", sources.RoleBusiness, ""); err != nil {
		t.Fatal(err)
	}
	for conn, key := range map[int64]string{ninja: "G1", sure: "s-biz"} {
		c, err := connections.ByID(w.d, conn)
		if err != nil {
			t.Fatal(err)
		}
		roles, _ := c.Options[sources.OptAccountRoles].(map[string]any)
		if roles[key] != string(sources.RoleBusiness) {
			t.Errorf("connection %d: roles %v", conn, roles)
		}
	}
}
