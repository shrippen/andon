package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/billing"
	"andon/internal/services/verbund"
	"andon/internal/testkit"
)

// Fake world: Kimai customer 7 "Acme GmbH" with two unbilled sheets,
// Invoice Ninja client "C1" of the same name with open invoice 101,
// and a Sure income that pays it.
//
//	Kimai  ──sheets 11,12──▶ Create ──POST invoices──▶ Ninja
//	Sure   ──income t1────▶ Book   ──POST payments──▶ Ninja
const (
	customerID = 7
	invoiceID  = 101
	txnID      = "t1"
	draftNo    = "2026-0042"
	invoiceNo  = "RE-2026-0017"
)

// writes records every non-GET request a fake service receives.
type writes struct {
	mu   sync.Mutex
	seen []string
	body map[string]map[string]any
}

// add stores one write as "METHOD /path" with its decoded JSON body.
func (w *writes) add(r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	w.seen = append(w.seen, key)
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	if w.body == nil {
		w.body = map[string]map[string]any{}
	}
	w.body[key] = m
}

// list returns the recorded writes.
func (w *writes) list() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.seen...)
}

// dayAgo formats today minus n days as YYYY-MM-DD.
func dayAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format(time.DateOnly)
}

// kimaiFake serves two unbilled sheets of customer 7.
func kimaiFake(t *testing.T, w *writes) string {
	t.Helper()
	sheet := func(id int64, day string) map[string]any {
		return map[string]any{
			"id": id, "begin": day + "T09:00:00+0200", "end": day + "T11:00:00+0200", "duration": 7200,
			"rate": 190, "hourlyRate": 95, "billable": true, "exported": false,
			"project":  map[string]any{"id": 3, "customer": map[string]any{"id": customerID}},
			"activity": map[string]any{"name": "Dev"}, "user": 1,
		}
	}
	get := map[string]any{
		"/api/timesheets":        []any{sheet(11, dayAgo(10)), sheet(12, dayAgo(9))},
		"/api/projects":          []any{map[string]any{"id": 3}},
		"/api/projects/3":        map[string]any{"id": 3, "name": "Website", "customer": customerID},
		"/api/customers":         []any{map[string]any{"id": customerID, "name": "Acme GmbH"}},
		"/api/timesheets/active": []any{},
	}
	return serve(t, w, func(r *http.Request) (any, bool) {
		if r.Method == http.MethodPatch {
			return map[string]any{"exported": true}, true
		}
		v, ok := get[r.URL.Path]
		return v, ok
	})
}

// ninjaFake serves client C1 (named client) with open invoice 101.
func ninjaFake(t *testing.T, w *writes, client string) string {
	t.Helper()
	page := func(items ...any) map[string]any {
		return map[string]any{"data": items, "meta": map[string]any{"pagination": map[string]any{"total_pages": 1}}}
	}
	invoice := map[string]any{"id": fmt.Sprint(invoiceID), "number": invoiceNo, "client_id": "C1", "status_id": "2",
		"date": dayAgo(20), "due_date": dayAgo(6), "amount": 119.5, "balance": 119.5}
	return serve(t, w, func(r *http.Request) (any, bool) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/invoices" {
			return map[string]any{"data": map[string]any{"id": "I9", "number": draftNo}}, true
		}
		if r.Method == http.MethodPost {
			return map[string]any{"data": map[string]any{}}, true
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/v1/") {
		case "invoices":
			return page(invoice), true
		case "clients":
			return page(map[string]any{"id": "C1", "name": client}), true
		}
		return page(), true
	})
}

// sureFake serves one income naming invoice 101's number.
func sureFake(t *testing.T, w *writes) string {
	t.Helper()
	txn := map[string]any{"id": txnID, "date": dayAgo(5), "name": invoiceNo + " Acme", "signed_amount_cents": 11950}
	get := map[string]any{
		"/api/v1/balance_sheet": map[string]any{"currency": "EUR"},
		"/api/v1/accounts":      map[string]any{"accounts": []any{}},
		"/api/v1/transactions":  map[string]any{"transactions": []any{txn}},
	}
	return serve(t, w, func(r *http.Request) (any, bool) {
		v, ok := get[r.URL.Path]
		return v, ok
	})
}

// serve starts a fake: writes are recorded, answer gives the JSON reply,
// unknown paths answer 404.
func serve(t *testing.T, w *writes, answer func(*http.Request) (any, bool)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.add(r)
		}
		v, ok := answer(r)
		if !ok {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(v)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// Create writes one Ninja draft with the customer's hours and, on
// request, flags every billed Kimai sheet as exported.
func TestCreateDraftsInvoiceAndMarksSheets(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var kimai, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &kimai))
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))

	number, err := billing.Create(context.Background(), d, who, space, 0, customerID, billing.MarkSheets, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if number != draftNo {
		t.Fatalf("number = %q", number)
	}

	if got := ninja.list(); len(got) != 1 || got[0] != "POST /api/v1/invoices" {
		t.Fatalf("ninja writes: %v", got)
	}
	body := ninja.body["POST /api/v1/invoices"]
	lines, _ := body["line_items"].([]any)
	if body["client_id"] != "C1" || len(lines) != 1 {
		t.Fatalf("draft body: %v", body)
	}
	line, _ := lines[0].(map[string]any)
	if line["product_key"] != "Website" || line["quantity"] != 4.0 || line["cost"] != 95.0 {
		t.Fatalf("draft line: %v", line)
	}

	exported := kimai.list()
	want := map[string]bool{"PATCH /api/timesheets/11/export": true, "PATCH /api/timesheets/12/export": true}
	if len(exported) != len(want) || !want[exported[0]] || !want[exported[1]] {
		t.Fatalf("kimai writes: %v", exported)
	}
}

// KeepSheets leaves Kimai untouched.
func TestCreateKeepSheetsSendsNoKimaiWrite(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var kimai, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &kimai))
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))

	if _, err := billing.Create(context.Background(), d, who, space, 0, customerID, billing.KeepSheets, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := kimai.list(); len(got) != 0 {
		t.Fatalf("kimai writes: %v", got)
	}
}

// Without a Ninja client of the customer's name, or without unbilled
// time, nothing is written.
func TestCreateRefusesUnknownClientOrCustomer(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var kimai, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &kimai))
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Someone Else"))
	ctx := context.Background()

	if _, err := billing.Create(ctx, d, who, space, 0, customerID, billing.MarkSheets, ""); !errors.Is(err, billing.ErrNoClient) {
		t.Fatalf("unknown client: %v", err)
	}
	if _, err := billing.Create(ctx, d, who, space, 0, 999, billing.MarkSheets, ""); !errors.Is(err, billing.ErrNothing) {
		t.Fatalf("unknown customer: %v", err)
	}
	if got := append(kimai.list(), ninja.list()...); len(got) != 0 {
		t.Fatalf("writes: %v", got)
	}
}

// Book pays the matched invoice with the income's amount and day.
func TestBookRecordsPayment(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var sure, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceSure, sureFake(t, &sure))
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))

	if err := billing.Book(context.Background(), d, who, space, 0, txnID, invoiceID, ""); err != nil {
		t.Fatalf("book: %v", err)
	}

	if got := ninja.list(); len(got) != 1 || got[0] != "POST /api/v1/payments" {
		t.Fatalf("ninja writes: %v", got)
	}
	body := ninja.body["POST /api/v1/payments"]
	invoices, _ := body["invoices"].([]any)
	if body["client_id"] != "C1" || body["amount"] != 119.5 || body["date"] != dayAgo(5) || len(invoices) != 1 {
		t.Fatalf("payment body: %v", body)
	}
	if inv, _ := invoices[0].(map[string]any); inv["invoice_id"] != fmt.Sprint(invoiceID) {
		t.Fatalf("payment invoice: %v", inv)
	}
}

// A stale proposal (other income or invoice) books nothing.
func TestBookRefusesStaleMatch(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var sure, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceSure, sureFake(t, &sure))
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))

	if err := billing.Book(context.Background(), d, who, space, 0, "t-other", invoiceID, ""); !errors.Is(err, billing.ErrNoMatch) {
		t.Fatalf("stale txn: %v", err)
	}
	if got := ninja.list(); len(got) != 0 {
		t.Fatalf("ninja writes: %v", got)
	}
}

// Somebody without edit rights on the space (a stranger, or a plain
// user in the instance space, who may only use it) neither drafts nor
// books; no service is written to.
func TestCreateAndBookNeedEditRight(t *testing.T) {
	d := testkit.DB(t)
	instance := &model.Space{Kind: enums.SpaceInstance, Name: "Instance", Version: 1}
	if err := content.AddSpace(d, instance); err != nil {
		t.Fatal(err)
	}
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	owner, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	var kimai, ninja, sure writes
	for _, target := range []struct {
		who   *access.Principal
		space int64
	}{{owner, space}, {boss, instance.ID}} {
		testkit.Conn(t, d, target.who, target.space, enums.ServiceKimai, kimaiFake(t, &kimai))
		testkit.Conn(t, d, target.who, target.space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))
		testkit.Conn(t, d, target.who, target.space, enums.ServiceSure, sureFake(t, &sure))
	}
	ctx := context.Background()

	for name, target := range map[string]int64{"stranger's space": space, "instance space": instance.ID} {
		if _, err := billing.Create(ctx, d, user, target, 0, customerID, billing.MarkSheets, ""); !errors.Is(err, access.ErrDenied) {
			t.Fatalf("create in %s: %v", name, err)
		}
		if err := billing.Book(ctx, d, user, target, 0, txnID, invoiceID, ""); !errors.Is(err, access.ErrDenied) {
			t.Fatalf("book in %s: %v", name, err)
		}
	}

	if got := append(append(kimai.list(), ninja.list()...), sure.list()...); len(got) != 0 {
		t.Fatalf("writes: %v", got)
	}
}

// Two Kimai, one Ninja in a Verbund with the second: drafts come only
// from that Kimai, and a draft names it; the other is not billed into a
// guessed Ninja.
func TestDraftsFollowVerbund(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var k1, k2, ninja writes
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &k1))
	second := testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &k2))
	ninjaID := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "Acme GmbH"))
	ctx := context.Background()

	// Both Kimai pair with the only Ninja: two pairs, a draft needs to say which.
	if _, err := billing.Create(ctx, d, who, space, 0, customerID, billing.KeepSheets, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("which kimai: %v", err)
	}

	if _, err := verbund.Create(d, who, "Firma", []int64{second, ninjaID}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := billing.Create(ctx, d, who, space, 0, customerID, billing.MarkSheets, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(k1.list()) != 0 || len(k2.list()) == 0 {
		t.Fatalf("marked sheets: kimai 1 %v, kimai 2 %v", k1.list(), k2.list())
	}
}

// Names differ ("Acme GmbH" in Kimai, "ACME Holding" in Ninja): no client
// until the Verbund links them; then the draft goes to that client.
func TestDraftUsesCustomerLink(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var kimai, ninja writes
	k := testkit.Conn(t, d, who, space, enums.ServiceKimai, kimaiFake(t, &kimai))
	n := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &ninja, "ACME Holding"))
	ctx := context.Background()

	if _, err := billing.Create(ctx, d, who, space, 0, customerID, billing.KeepSheets, ""); !errors.Is(err, billing.ErrNoClient) {
		t.Fatalf("by name: %v", err)
	}
	id, err := verbund.Create(d, who, "Firma", []int64{k, n}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := verbund.LinkCustomer(context.Background(), d, who, id, customerID, "C1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := billing.Create(ctx, d, who, space, 0, customerID, billing.KeepSheets, ""); err != nil {
		t.Fatalf("linked: %v", err)
	}
	if body := ninja.body["POST /api/v1/invoices"]; body["client_id"] != "C1" {
		t.Fatalf("invoice: %+v", body)
	}
}
