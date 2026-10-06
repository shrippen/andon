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

// financeFakes: Sure with incomes from "ACME GMBH" and "Treuhand Nord",
// Paperless with correspondents ACME (7) and Lieferant (9).
func financeFakes(t *testing.T) (string, string) {
	t.Helper()
	sure := http.NewServeMux()
	sure.HandleFunc("GET /api/v1/{what}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("what") {
		case "transactions":
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`{"transactions": []}`))
				return
			}
			w.Write([]byte(`{"transactions": [
				{"id": "t1", "date": "2026-09-01", "name": "Gutschrift", "signed_amount_cents": 95200, "merchant": {"name": "ACME GMBH"}},
				{"id": "t2", "date": "2026-09-02", "name": "Gutschrift", "signed_amount_cents": 12000, "merchant": {"name": "Treuhand Nord"}},
				{"id": "t3", "date": "2026-09-03", "name": "Miete", "signed_amount_cents": -80000, "merchant": {"name": "Vermieter"}}]}`))
		default:
			w.Write([]byte(`{}`))
		}
	})
	docs := http.NewServeMux()
	docs.HandleFunc("GET /api/{what}/", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("what") == "correspondents" {
			w.Write([]byte(`{"count": 2, "results": [{"id": 7, "name": "ACME", "document_count": 3}, {"id": 9, "name": "Lieferant", "document_count": 1}]}`))
			return
		}
		w.Write([]byte(`{"count": 0, "results": []}`))
	})
	s, p := httptest.NewServer(sure), httptest.NewServer(docs)
	t.Cleanup(s.Close)
	t.Cleanup(p.Close)
	return s.URL, p.URL
}

// rowOf is the row of a Ninja client.
func rowOf(t *testing.T, v verbund.CustomerView, key string) verbund.CustomerRow {
	t.Helper()
	for _, r := range v.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row %s", key)
	return verbund.CustomerRow{}
}

// cellOf is a row's cell of a service.
func cellOf(t *testing.T, v verbund.CustomerView, r verbund.CustomerRow, s enums.ServiceType) verbund.Cell {
	t.Helper()
	for i, c := range v.Columns {
		if c.Service == s {
			return r.Cells[i]
		}
	}
	t.Fatalf("no column %s", s)
	return verbund.Cell{}
}

// Invoice Ninja leads: each client gets its Kimai customer, Sure payer
// and Paperless correspondent; suggestions until confirmed.
func TestCustomers(t *testing.T) {
	w := newWorld(t)
	var got renames
	kURL, nURL := customerFakes(t, &got)
	sURL, pURL := financeFakes(t)
	kimai := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceKimai, kURL)
	ninja := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceInvoiceNinja, nURL)
	sure := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceSure, sURL)
	docs := testkit.Conn(t, w.d, w.user, w.own, enums.ServicePaperless, pURL)
	fetch(t, w, kimai, ninja, sure, docs)
	id, err := verbund.Create(w.d, w.user, "Firma", []int64{kimai, ninja, sure, docs}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	v, err := verbund.Customers(ctx, w.d, w.user, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Hub.Service != enums.ServiceInvoiceNinja || len(v.Columns) != 3 || !v.CanAlign {
		t.Fatalf("view: hub %s columns %d", v.Hub.Service, len(v.Columns))
	}
	acme := rowOf(t, v, "Kx9")
	if c := cellOf(t, v, acme, enums.ServiceKimai); c.State != verbund.CustomerSuggested || c.Key != "12" {
		t.Fatalf("acme kimai: %+v", c)
	}
	if c := cellOf(t, v, acme, enums.ServiceSure); c.State != verbund.CustomerSuggested || c.Key != "ACME GMBH" {
		t.Fatalf("acme sure: %+v", c)
	}
	if c := cellOf(t, v, acme, enums.ServicePaperless); c.State != verbund.CustomerSuggested || c.Key != "7" {
		t.Fatalf("acme paperless: %+v", c)
	}
	// Only incomes are payers: the rent's landlord is none.
	for _, c := range v.Columns {
		for _, p := range c.Parties {
			if p.Name == "Vermieter" {
				t.Fatalf("expense payer listed: %+v", c.Parties)
			}
		}
	}
	if m, _ := verbund.ClientMapFor(w.d, kimai, ninja); len(m) != 0 {
		t.Fatalf("stored before confirm: %v", m)
	}

	// Beta: Kimai suggested, Sure and Paperless open. Confirm stores 4.
	if n, err := verbund.ConfirmSuggestions(ctx, w.d, w.user, id, ""); err != nil || n != 4 {
		t.Fatalf("confirm: %d %v", n, err)
	}
	m, err := verbund.ClientMapFor(w.d, kimai, ninja)
	if err != nil || m[12] != "Kx9" || m[15] != "Zz1" || len(m) != 2 {
		t.Fatalf("client map: %v %v", m, err)
	}
	payers, err := verbund.PayerMapFor(w.d, sure, ninja)
	if err != nil || payers["ACME GMBH"] != "Kx9" || len(payers) != 1 {
		t.Fatalf("payers: %v %v", payers, err)
	}
	docsMap, err := verbund.DocsMapFor(w.d, ninja, docs)
	if err != nil || docsMap["Kx9"] != 7 {
		t.Fatalf("docs: %v %v", docsMap, err)
	}

	// Beta's payer by hand; "Treuhand Nord" pays for Beta.
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", sure, "Treuhand Nord", ""); err != nil {
		t.Fatal(err)
	}
	// Beta has no documents in Paperless: said so.
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", docs, "", ""); err != nil {
		t.Fatal(err)
	}
	v, _ = verbund.Customers(ctx, w.d, w.user, id)
	beta := rowOf(t, v, "Zz1")
	if c := cellOf(t, v, beta, enums.ServicePaperless); c.State != verbund.CustomerNone {
		t.Fatalf("beta paperless: %+v", c)
	}
	if v.Suggested() {
		t.Fatal("suggestions left")
	}

	// One payer, one client; only what the services have.
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", sure, "ACME GMBH", ""); !errors.Is(err, verbund.ErrClientTaken) {
		t.Fatalf("taken: %v", err)
	}
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", sure, "Vermieter", ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("unknown payer: %v", err)
	}
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "gone", sure, "", ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("unknown client: %v", err)
	}
	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", 99999, "", ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("unknown column: %v", err)
	}

	// Ninja holds the names: align writes "ACME GmbH" to Kimai 12.
	if c := cellOf(t, v, rowOf(t, v, "Kx9"), enums.ServiceKimai); !c.NameDiffers {
		t.Fatalf("acme differs: %+v", c)
	}
	if err := verbund.AlignName(ctx, w.d, w.user, id, "Kx9", ""); err != nil {
		t.Fatal(err)
	}
	got.mu.Lock()
	if len(got.list) != 1 || got.list[0]["name"] != "ACME GmbH" || got.list[0]["id"] != "12" {
		t.Fatalf("renames: %+v", got.list)
	}
	got.mu.Unlock()

	// Unlinking one cell keeps the others; unlinking all drops the entry.
	if err := verbund.UnlinkCustomer(w.d, w.user, id, "Zz1", kimai, ""); err != nil {
		t.Fatal(err)
	}
	v, _ = verbund.Customers(ctx, w.d, w.user, id)
	beta = rowOf(t, v, "Zz1")
	if c := cellOf(t, v, beta, enums.ServiceKimai); c.State != verbund.CustomerSuggested {
		t.Fatalf("beta kimai after unlink: %+v", c)
	}
	if c := cellOf(t, v, beta, enums.ServiceSure); c.State != verbund.CustomerConfirmed {
		t.Fatalf("beta sure after kimai unlink: %+v", c)
	}
	for _, conn := range []int64{sure, docs} {
		if err := verbund.UnlinkCustomer(w.d, w.user, id, "Zz1", conn, ""); err != nil {
			t.Fatal(err)
		}
	}
	if payers, _ := verbund.PayerMapFor(w.d, sure, ninja); len(payers) != 1 {
		t.Fatalf("payers after unlink: %v", payers)
	}
}

// Kimai "Intern", stored as having no Ninja client before Ninja led the
// page, shows as an orphan; linking it to a client moves it there.
func TestCustomersOrphan(t *testing.T) {
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
	if err := verbund.StoreEntryForTest(w.d, id, []verbund.TestKey{{ConnID: kimai, Key: "17"}, {ConnID: ninja, None: true}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	v, err := verbund.Customers(ctx, w.d, w.user, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Orphans) != 1 || v.Orphans[0].Name != "Intern" {
		t.Fatalf("orphans: %+v", v.Orphans)
	}
	if m, _ := verbund.ClientMapFor(w.d, kimai, ninja); m[17] != "" || len(m) != 1 {
		t.Fatalf("map: %v", m)
	}

	if err := verbund.LinkCustomer(ctx, w.d, w.user, id, "Zz1", kimai, "17", ""); err != nil {
		t.Fatal(err)
	}
	v, _ = verbund.Customers(ctx, w.d, w.user, id)
	if len(v.Orphans) != 0 {
		t.Fatalf("orphan stayed: %+v", v.Orphans)
	}
	if m, _ := verbund.ClientMapFor(w.d, kimai, ninja); m[17] != "Zz1" {
		t.Fatalf("map after move: %v", m)
	}

	// An orphan can be forgotten.
	if err := verbund.StoreEntryForTest(w.d, id, []verbund.TestKey{{ConnID: kimai, Key: "15"}, {ConnID: ninja, None: true}}); err != nil {
		t.Fatal(err)
	}
	v, _ = verbund.Customers(ctx, w.d, w.user, id)
	if len(v.Orphans) != 1 {
		t.Fatalf("orphans: %+v", v.Orphans)
	}
	if err := verbund.RemoveOrphan(w.d, w.user, id, v.Orphans[0].EntryID, ""); err != nil {
		t.Fatal(err)
	}
	if err := verbund.RemoveOrphan(w.d, w.user, id, v.Orphans[0].EntryID, ""); !errors.Is(err, verbund.ErrUnknownCustomer) {
		t.Fatalf("twice: %v", err)
	}
}

// Without Invoice Ninja, Kimai leads (Kimai and Sure).
func TestCustomersKimaiLeads(t *testing.T) {
	w := newWorld(t)
	var got renames
	kURL, _ := customerFakes(t, &got)
	sURL, _ := financeFakes(t)
	kimai := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceKimai, kURL)
	sure := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceSure, sURL)
	fetch(t, w, kimai, sure)
	id, err := verbund.Create(w.d, w.user, "Firma", []int64{kimai, sure}, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := verbund.Customers(context.Background(), w.d, w.user, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Hub.Service != enums.ServiceKimai || len(v.Columns) != 1 || v.CanAlign {
		t.Fatalf("view: %+v", v.Hub.Service)
	}
	if c := cellOf(t, v, rowOf(t, v, "12"), enums.ServiceSure); c.State != verbund.CustomerSuggested || c.Key != "ACME GMBH" {
		t.Fatalf("acme sure: %+v", c)
	}
}

// Only editors of every member change links; a Verbund with one member
// that knows customers has none.
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
	if err := verbund.LinkCustomer(context.Background(), w.d, w.user, id, "Kx9", kimai, "12", ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("user links: %v", err)
	}

	daw := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceDawarich, "https://d.example")
	sure := testkit.Conn(t, w.d, w.user, w.own, enums.ServiceSure, "https://s.example")
	other, err := verbund.Create(w.d, w.user, "Reisen", []int64{daw, sure}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verbund.Customers(context.Background(), w.d, w.user, other); !errors.Is(err, verbund.ErrNoCustomers) {
		t.Fatalf("one member: %v", err)
	}
}
