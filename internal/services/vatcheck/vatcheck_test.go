package vatcheck_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/vatcheck"
	"andon/internal/services/workbench"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// Fake Invoice Ninja, this month: four open transactions.
//
//	b1  debit  39.95  Elbnetz        expense e2 waits   → link
//	b2  debit  899.00 Kabelwerk      vendor v1 known    → create
//	b3  debit  41.65  NH Billing     unknown, Sure helps → create, new vendor
//	b4  credit 238.00 R-017          open invoice i7    → book
const (
	vendorNew = "V9"
	expNew    = "E9"
)

// writes records every non-GET request with its JSON body.
type writes struct {
	mu   sync.Mutex
	seen []string
	body []map[string]any
}

func (w *writes) add(r *http.Request) map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	w.seen = append(w.seen, r.Method+" "+r.URL.Path)
	w.body = append(w.body, m)
	return m
}

func (w *writes) list() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.seen)
}

// day is the n-th day of this month.
func day(n int) string {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), n, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

func page(items ...any) map[string]any {
	return map[string]any{"data": items, "meta": map[string]any{"pagination": map[string]any{"total_pages": 1}}}
}

func ninjaFake(t *testing.T, w *writes, b1Status string) string {
	t.Helper()
	txn := func(id, date, payer, text, kind string, amount float64, status string) map[string]any {
		return map[string]any{"id": id, "date": date, "participant_name": payer, "description": text, "base_type": kind, "amount": amount, "status_id": status,
			"bank_integration_id": "a1"}
	}
	private := txn("b5", day(2), "DB Fernverkehr AG", "Deutschlandticket", "DEBIT", 58, "1")
	private["bank_integration_id"] = "a2"
	get := map[string]any{
		"bank_transactions": page(
			txn("b1", day(3), "Elbnetz Mobilfunk", "", "DEBIT", 39.95, b1Status),
			txn("b2", day(4), "Kabelwerk Studiobedarf", "KW-56107", "DEBIT", 899, "1"),
			txn("b3", day(5), "NH Billing", "Rechnung", "DEBIT", 41.65, "1"),
			txn("b4", day(6), "Northlight", "R-2026-017", "CREDIT", 238, "1"),
			private),
		"bank_integrations": page(map[string]any{"id": "a1", "bank_account_name": "Girokonto", "bank_account_number": "DE001111"},
			map[string]any{"id": "a2", "bank_account_name": "Girokonto", "bank_account_number": "DE002222"}),
		"expenses": page(
			map[string]any{"id": "e1", "number": "EX-1", "vendor_id": "v1", "category_id": "c1", "date": day(1), "amount": 89.7,
				"tax_name1": "MwSt.", "tax_rate1": 19, "uses_inclusive_taxes": true, "transaction_id": "b0"},
			map[string]any{"id": "e2", "number": "EX-2", "vendor_id": "v2", "date": day(2), "amount": 39.95}),
		"vendors":            page(map[string]any{"id": "v1", "name": "Kabelwerk Studiobedarf"}, map[string]any{"id": "v2", "name": "Elbnetz Mobilfunk"}),
		"expense_categories": page(map[string]any{"id": "c1", "name": "Technik"}, map[string]any{"id": "c2", "name": "Hosting"}),
		"tax_rates":          page(map[string]any{"name": "MwSt.", "rate": 19}),
		"invoices":           page(map[string]any{"id": "i7", "number": "R-2026-017", "client_id": "C1", "status_id": "2", "date": day(1), "amount": 238, "balance": 238}),
		"clients":            page(map[string]any{"id": "C1", "name": "Northlight Pictures"}),
	}
	return serve(t, func(rw http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		if r.Method == http.MethodGet {
			v, ok := get[path]
			if !ok {
				v = page()
			}
			_ = json.NewEncoder(rw).Encode(v)
			return
		}
		body := w.add(r)
		switch path {
		case "vendors":
			_ = json.NewEncoder(rw).Encode(map[string]any{"data": map[string]any{"id": vendorNew}})
		case "expenses":
			_ = json.NewEncoder(rw).Encode(map[string]any{"data": map[string]any{"id": expNew, "number": "EX-9"}})
		case "bank_transactions/match":
			list, _ := body["transactions"].([]any)
			item, _ := list[0].(map[string]any)
			_ = json.NewEncoder(rw).Encode(map[string]any{"data": []any{map[string]any{"id": item["id"], "status_id": "3"}}})
		case "bank_transactions/bulk":
			ids, _ := body["ids"].([]any)
			_ = json.NewEncoder(rw).Encode(map[string]any{"data": []any{map[string]any{"id": ids[0], "archived_at": 1760000000}}})
		default:
			http.NotFound(rw, r)
		}
	})
}

// sureFake has b2 and b3: Sure names b3's merchant and category.
func sureFake(t *testing.T) string {
	t.Helper()
	get := map[string]any{
		"/api/v1/balance_sheet": map[string]any{"currency": "EUR"},
		"/api/v1/accounts":      map[string]any{"accounts": []any{}},
		"/api/v1/transactions": map[string]any{"transactions": []any{
			map[string]any{"id": "s1", "date": day(4), "name": "Kabelwerk", "signed_amount_cents": -89900, "account": map[string]any{"name": "Geschäft"}},
			map[string]any{"id": "s2", "date": day(5), "name": "NH BILLING", "signed_amount_cents": -4165, "account": map[string]any{"name": "Geschäft"},
				"merchant": map[string]any{"name": "Nordhost Server"}, "category": map[string]any{"name": "Hosting"}, "tags": []any{map[string]any{"name": "Firma"}}},
		}},
	}
	return serve(t, func(rw http.ResponseWriter, r *http.Request) {
		v, ok := get[r.URL.Path]
		if !ok {
			http.NotFound(rw, r)
			return
		}
		_ = json.NewEncoder(rw).Encode(v)
	})
}

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		h(rw, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func thisMonth() string { return time.Now().UTC().Format("2006-01") }

func TestOpenProposesEachCase(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var w writes
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &w, "1"))
	testkit.Conn(t, d, who, space, enums.ServiceSure, sureFake(t))

	p, err := vatcheck.Open(context.Background(), d, who, "", thisMonth(), time.Now().UTC())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if p.Conn != conn || p.Bank == "" || len(p.Cases) != 5 || p.SetUp {
		t.Fatalf("page: conn %d bank %q cases %d", p.Conn, p.Bank, len(p.Cases))
	}
	// Without business accounts named, every account counts.
	want := []metrics.BankAction{metrics.ActionCreate, metrics.ActionLink, metrics.ActionCreate, metrics.ActionCreate, metrics.ActionInvoice}
	for i, c := range p.Cases {
		if c.Action != want[i] {
			t.Errorf("%s: action %q, want %q", c.Txn.Key, c.Action, want[i])
		}
	}
	if dr := p.Cases[3].Draft; dr.Vendor != "Nordhost Server" || dr.CategoryKey != "c2" || p.Cases[3].Sure == nil || p.Cases[3].Sure.Tags[0] != "Firma" {
		t.Errorf("b3 draft from Sure: %+v", dr)
	}
	if len(w.list()) != 0 {
		t.Fatalf("opening wrote: %v", w.list())
	}
}

// Review spells out the writes; Apply makes exactly those, archiving the
// private one in Invoice Ninja.
func TestApplyWritesConfirmedSteps(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var w writes
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &w, "1"))
	ctx := context.Background()
	steps := []workbench.Step{
		{Case: "b1", Action: string(metrics.ActionLink), Fields: map[string]string{vatcheck.FieldExpense: "e2"}},
		{Case: "b3", Action: string(metrics.ActionCreate), Fields: map[string]string{vatcheck.FieldVendor: "Nordhost Server", vatcheck.FieldCategory: "c2", vatcheck.FieldTax: "19"}},
		{Case: "b4", Action: string(metrics.ActionInvoice), Fields: map[string]string{vatcheck.FieldInvoice: "i7"}},
		{Case: "b2", Action: workbench.ActionSkip},
	}

	plan, err := vatcheck.Review(ctx, d, who, scopeOf(conn), steps)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if plan.Writes() != 4 || !plan.Steps[1].Item.NewVendor || plan.Steps[1].Item.Tax != 6.65 || plan.Steps[1].Item.Category != "Hosting" {
		t.Fatalf("plan: %+v", plan.Steps)
	}
	if len(w.list()) != 0 {
		t.Fatalf("review wrote: %v", w.list())
	}

	res, err := vatcheck.Apply(ctx, d, who, scopeOf(conn), steps, "")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Done != 3 || res.Skipped != 1 || len(res.Failed) != 0 {
		t.Fatalf("result: %+v", res)
	}
	got := w.list()
	want := []string{"POST /api/v1/bank_transactions/match", "POST /api/v1/vendors", "POST /api/v1/expenses",
		"POST /api/v1/bank_transactions/match", "POST /api/v1/bank_transactions/match", "POST /api/v1/bank_transactions/bulk"}
	if !slices.Equal(got, want) {
		t.Fatalf("writes %v, want %v", got, want)
	}
	expense := w.body[2]
	if expense["vendor_id"] != vendorNew || expense["category_id"] != "c2" || expense["tax_rate1"] != 19.0 || expense["uses_inclusive_taxes"] != true {
		t.Errorf("expense body: %v", expense)
	}
	link := w.body[3]["transactions"].([]any)[0].(map[string]any)
	if link["id"] != "b3" || link["expense_id"] != expNew {
		t.Errorf("link of the new expense: %v", link)
	}
	if pay := w.body[4]["transactions"].([]any)[0].(map[string]any); pay["invoice_ids"] != "i7" {
		t.Errorf("invoice booking: %v", pay)
	}

	if archive := w.body[5]; archive["action"] != "archive" || archive["ids"].([]any)[0] != "b2" {
		t.Errorf("archive body: %v", archive)
	}
}

// Roles named per account: the private account's train pass waits apart
// until its vendor has expenses; two accounts of one name are told apart
// by their number.
func TestAccounts(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var w writes
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &w, "1"))
	ctx := context.Background()

	p, err := vatcheck.Open(ctx, d, who, scopeOf(conn), thisMonth(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Accounts) != 2 || p.Accounts[0].Name != "Girokonto ··1111" || p.Accounts[1].Name != "Girokonto ··2222" || !p.CanSet {
		t.Fatalf("accounts: %+v can set %v", p.Accounts, p.CanSet)
	}

	roles := []vatcheck.Role{{ConnID: conn, Key: "a1", Role: sources.RoleBusiness}, {ConnID: conn, Key: "a2", Role: sources.RolePrivate}}
	if err := vatcheck.SaveRoles(ctx, d, who, scopeOf(conn), roles, ""); err != nil {
		t.Fatal(err)
	}
	p, err = vatcheck.Open(ctx, d, who, scopeOf(conn), thisMonth(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !p.SetUp || len(p.Cases) != 4 || len(p.Apart) != 1 || !p.Apart[0].Private || p.Apart[0].Txn.Key != "b5" {
		t.Fatalf("set up %v, cases %d, apart %+v", p.SetUp, len(p.Cases), p.Apart)
	}
	if p.Accounts[0].Role != sources.RoleBusiness || p.Accounts[1].Role != sources.RolePrivate {
		t.Errorf("roles: %+v", p.Accounts)
	}
}

// A transaction booked meanwhile is not booked twice.
func TestApplySkipsBookedMeanwhile(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var w writes
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &w, "3"))

	res, err := vatcheck.Apply(context.Background(), d, who, scopeOf(conn), []workbench.Step{{Case: "b1", Action: string(metrics.ActionLink), Fields: map[string]string{vatcheck.FieldExpense: "e2"}}}, "")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Done != 0 || len(res.Failed) != 1 || res.Failed[0].Reason != string(vatcheck.ProblemGone) || len(w.list()) != 0 {
		t.Fatalf("result %+v, writes %v", res, w.list())
	}
}

// The demo shows every kind of case and takes no writes.
func TestDemo(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	testkit.Conn(t, d, who, space, enums.ServiceSure, "demo://sure")
	ctx := context.Background()
	today := time.Now().UTC()

	seen := map[metrics.BankAction]bool{}
	for _, s := range metrics.VATSpans(today, "monthly", 2) {
		p, err := vatcheck.Open(ctx, d, who, scopeOf(conn), s.Key, today)
		if err != nil {
			t.Fatalf("open %s: %v", s.Key, err)
		}
		for _, c := range p.Cases {
			seen[c.Action] = true
		}
	}
	for _, a := range []metrics.BankAction{metrics.ActionLink, metrics.ActionCreate} {
		if !seen[a] {
			t.Errorf("demo lacks a %q case: %v", a, seen)
		}
	}

	_, err := vatcheck.Apply(ctx, d, who, scopeOf(conn), []workbench.Step{{Case: "demo-b-studio", Action: string(metrics.ActionCreate), Fields: map[string]string{vatcheck.FieldVendor: "Kabelwerk"}}}, "")
	if !errors.Is(err, vatcheck.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}

	// Marked as filed, the figures are kept; unchanged, nothing is due.
	span := metrics.VATSpans(today, "monthly", 1)[0].Key
	if err := vatcheck.FileReturn(ctx, d, who, scopeOf(conn), span, today, ""); err != nil {
		t.Fatal(err)
	}
	p, err := vatcheck.Open(ctx, d, who, scopeOf(conn), span, today)
	if err != nil || !p.Filed || len(p.Changed) != 0 || len(p.Elster.Figures()) == 0 {
		t.Fatalf("filed: %v changed %+v %v", p.Filed, p.Changed, err)
	}
}

// Until the return is due the last period opens, afterwards this one.
func TestDefaultSpan(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	var w writes
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, ninjaFake(t, &w, "1"))
	ctx := context.Background()

	for _, c := range []struct {
		today time.Time
		want  string
	}{
		{time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), "2026-09"},
		{time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), "2026-10"},
	} {
		p, err := vatcheck.Open(ctx, d, who, "", "", c.today)
		if err != nil {
			t.Fatal(err)
		}
		if p.Span.Key != c.want {
			t.Errorf("%s: span %s, want %s", c.today.Format(time.DateOnly), p.Span.Key, c.want)
		}
	}
}

// scopeOf is the scope of a lone Invoice Ninja (workbench.Scope.Key).
func scopeOf(conn int64) string { return "c" + strconv.FormatInt(conn, 10) }

// The period cards run oldest first and end with the running one; the
// chosen card counts what the page counts, and a filed period says so.
func TestCards(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	testkit.Conn(t, d, who, space, enums.ServiceSure, "demo://sure")
	ctx := context.Background()
	today := time.Now().UTC()
	last := metrics.VATSpans(today, "monthly", 2)[1]

	p, err := vatcheck.Open(ctx, d, who, scopeOf(conn), last.Key, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != len(p.Spans) || p.Cards[len(p.Cards)-1].State != vatcheck.SpanRunning || p.Cards[len(p.Cards)-1].Key != p.Spans[0].Key {
		t.Fatalf("cards: %+v", p.Cards)
	}
	byKey := map[string]vatcheck.SpanCard{}
	for _, c := range p.Cards {
		byKey[c.Key] = c
	}
	if c := byKey[last.Key]; c.Open != p.Check.Open || c.Amount != p.Check.Due || c.State == vatcheck.SpanRunning || c.State == vatcheck.SpanFiled {
		t.Errorf("chosen card %+v, check %+v", c, p.Check)
	}

	if err := vatcheck.FileReturn(ctx, d, who, scopeOf(conn), last.Key, today, ""); err != nil {
		t.Fatal(err)
	}
	if p, err = vatcheck.Open(ctx, d, who, scopeOf(conn), last.Key, today); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Cards {
		if c.Key == last.Key && c.State != vatcheck.SpanFiled {
			t.Errorf("filed card: %+v", c)
		}
	}
}

// On its deadline day the return is still open, not yet unfiled: the
// deadline runs through its whole day, as a period does.
func TestCardDueDay(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), 10, 15, 0, 0, 0, time.UTC)
	last := metrics.VATSpans(today, "monthly", 2)[1]

	p, err := vatcheck.Open(context.Background(), d, who, scopeOf(conn), last.Key, today)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Cards {
		if c.Key == last.Key && c.State != vatcheck.SpanOpen {
			t.Errorf("due day %s: state %s, due %s", today.Format(time.DateOnly), c.State, c.Due)
		}
	}
}
