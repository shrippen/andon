package verbund_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/testkit"
)

// renames records the customer renames a fake Kimai receives.
type renames struct {
	mu   sync.Mutex
	list []map[string]any
}

// customerFakes: Kimai customers Acme (12), Beta (15), Intern (17);
// Ninja clients "ACME GmbH" (Kx9) and "Beta KG" (Zz1).
func customerFakes(t *testing.T, got *renames) (string, string) {
	t.Helper()
	kimai := http.NewServeMux()
	for path, body := range map[string]string{"/api/timesheets": `[]`, "/api/timesheets/active": `[]`, "/api/projects": `[]`,
		"/api/customers": `[{"id": 12, "name": "Acme"}, {"id": 15, "name": "Beta"}, {"id": 17, "name": "Intern"}]`} {
		kimai.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
	}
	kimai.HandleFunc("PATCH /api/customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		m["id"] = r.PathValue("id")
		got.mu.Lock()
		got.list = append(got.list, m)
		got.mu.Unlock()
		w.Write([]byte(`{}`))
	})
	page := func(items string) string {
		return `{"data": [` + items + `], "meta": {"pagination": {"total_pages": 1}}}`
	}
	ninja := http.NewServeMux()
	ninja.HandleFunc("GET /api/v1/{what}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("what") == "clients" {
			w.Write([]byte(page(`{"id": "Kx9", "name": "ACME GmbH"}, {"id": "Zz1", "name": "Beta KG"}`)))
			return
		}
		w.Write([]byte(page("")))
	})
	k, n := httptest.NewServer(kimai), httptest.NewServer(ninja)
	t.Cleanup(k.Close)
	t.Cleanup(n.Close)
	return k.URL, n.URL
}

// fetch fills the background results the view reads.
func fetch(t *testing.T, w world, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		c, err := connections.ByID(w.d, id)
		if err != nil {
			t.Fatal(err)
		}
		key := c.Service + ".data"
		if _, err := svcdata.Get(context.Background(), w.d, key, nil, c, model.UserHolder(w.user.UserID), svcdata.Force); err != nil {
			t.Fatal(err)
		}
	}
}

func rowOf(t *testing.T, v verbund.CustomerView, id int64) verbund.CustomerRow {
	t.Helper()
	for _, r := range v.Rows {
		if r.KimaiID == id {
			return r
		}
	}
	t.Fatalf("no row %d", id)
	return verbund.CustomerRow{}
}

func TestCustomers(t *testing.T) {
	w := newWorld(t)
	var got renames
	kURL, nURL := customerFakes(t, &got)
	kimai := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceKimai, kURL)
	ninja := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceInvoiceNinja, nURL)
	fetch(t, w, kimai, ninja)
	id, err := verbund.Create(w.d, w.user, "Firma", []int64{kimai, ninja}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Suggestions by name, nothing stored yet; the name decides meanwhile.
	v, err := verbund.Customers(ctx, w.d, w.user, id)
	if err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, v, 12); r.State != verbund.CustomerSuggested || r.ClientKey != "Kx9" {
		t.Fatalf("acme: %+v", r)
	}
	if r := rowOf(t, v, 17); r.State != verbund.CustomerOpen {
		t.Fatalf("intern: %+v", r)
	}
	if m, _ := verbund.ClientMapFor(w.d, kimai, ninja); len(m) != 0 {
		t.Fatalf("stored before confirm: %v", m)
	}

	if n, err := verbund.ConfirmSuggestions(ctx, w.d, w.user, id, ""); err != nil || n != 2 {
		t.Fatalf("confirm: %d %v", n, err)
	}
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, 17, "", ""); err != nil {
		t.Fatal(err)
	}
	m, err := verbund.ClientMapFor(w.d, kimai, ninja)
	if err != nil || m[12] != "Kx9" || m[15] != "Zz1" || m[17] != "" || len(m) != 3 {
		t.Fatalf("map: %v %v", m, err)
	}

	// One client, one customer.
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, 15, "Kx9", ""); !errors.Is(err, verbund.ErrClientTaken) {
		t.Fatalf("taken: %v", err)
	}
	// Only what the services have: no foreign client, no foreign customer.
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, 15, "gone", ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("unknown client: %v", err)
	}
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, 999, "", ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("unknown customer: %v", err)
	}

	// Ninja holds the names: align writes "ACME GmbH" to Kimai 12.
	v, _ = verbund.Customers(ctx, w.d, w.user, id)
	if r := rowOf(t, v, 12); !r.NameDiffers {
		t.Fatalf("acme differs: %+v", r)
	}
	if err := verbund.AlignName(ctx, w.d, w.user, id, 12, ""); err != nil {
		t.Fatal(err)
	}
	got.mu.Lock()
	if len(got.list) != 1 || got.list[0]["name"] != "ACME GmbH" || got.list[0]["id"] != "12" {
		t.Fatalf("renames: %+v", got.list)
	}
	got.mu.Unlock()

	// Unlinked, Beta is a suggestion again.
	if err := verbund.UnlinkCustomer(w.d, w.user, id, 15, ""); err != nil {
		t.Fatal(err)
	}
	if v, err = verbund.Customers(ctx, w.d, w.user, id); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, v, 15); r.State != verbund.CustomerSuggested {
		t.Fatalf("beta after unlink: %+v", r)
	}
}

// Only editors of every member change links; a Verbund without Ninja
// has no customers.
func TestCustomersRights(t *testing.T) {
	w := newWorld(t)
	var got renames
	kURL, nURL := customerFakes(t, &got)
	kimai := testkit.Conn(t, w.d, w.admin, w.instance, enums.ServiceKimai, kURL)
	ninja := testkit.Conn(t, w.d, w.admin, w.instance, enums.ServiceInvoiceNinja, nURL)
	id, err := verbund.Create(w.d, w.admin, "Firma", []int64{kimai, ninja}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := verbund.LinkCustomer(context.Background(), w.d, w.user, id, 12, "Kx9", ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("user links: %v", err)
	}

	daw := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceDawarich, "https://d.example")
	sure := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceSure, "https://s.example")
	other, err := verbund.Create(w.d, w.user, "Reisen", []int64{daw, sure}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verbund.Customers(context.Background(), w.d, w.user, other); !errors.Is(err, verbund.ErrNoCustomers) {
		t.Fatalf("no ninja: %v", err)
	}
}
