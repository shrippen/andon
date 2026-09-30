package outbound_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"andon/internal/outbound"
)

// token is the caller's API token every fake expects.
const token = "tok"

// call is one request a fake service saw.
type call struct {
	Method, Path string
	Header       http.Header
	Body         []byte
}

// json decodes the call's body into a generic map.
func (c call) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("decode body %q: %v", c.Body, err)
	}
	return m
}

// fake records every request and answers with reply(call).
type fake struct {
	mu    sync.Mutex
	calls []call
}

// serve starts a server that records requests; reply writes the answer.
func (f *fake) serve(t *testing.T, reply func(w http.ResponseWriter, c call)) outbound.Target {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		reply(w, c)
	}))
	t.Cleanup(srv.Close)
	return outbound.Target{URL: srv.URL, Token: token, VerifyTLS: true}
}

// one returns the single recorded call, failing on any other count.
func (f *fake) one(t *testing.T) call {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(f.calls), f.calls)
	}
	return f.calls[0]
}

// answer writes status and a JSON body.
func answer(status int, body string) func(http.ResponseWriter, call) {
	return func(w http.ResponseWriter, _ call) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// assertNinja checks Invoice Ninja's auth and JSON headers.
func assertNinja(t *testing.T, c call, method, path string) {
	t.Helper()
	if c.Method != method || c.Path != path {
		t.Fatalf("expected %s %s, got %s %s", method, path, c.Method, c.Path)
	}
	if got := c.Header.Get("X-API-TOKEN"); got != token {
		t.Fatalf("X-API-TOKEN = %q", got)
	}
	if got := c.Header.Get("X-Requested-With"); got != "XMLHttpRequest" {
		t.Fatalf("X-Requested-With = %q", got)
	}
	if got := c.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

// roundTrip normalises v through JSON so it compares with decoded bodies.
func roundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNinjaPaymentPostsInvoiceAllocation(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"data":{"id":"P1"}}`))

	if err := outbound.NinjaPayment(context.Background(), to, "C1", "I1", 119.5, "2026-09-01", "Bank 42"); err != nil {
		t.Fatalf("payment: %v", err)
	}

	c := f.one(t)
	assertNinja(t, c, http.MethodPost, "/api/v1/payments")
	want := roundTrip(t, map[string]any{
		"client_id": "C1", "amount": 119.5, "date": "2026-09-01", "transaction_reference": "Bank 42",
		"invoices": []any{map[string]any{"invoice_id": "I1", "amount": 119.5}},
	})
	if got := c.json(t); !reflect.DeepEqual(any(got), want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
}

func TestNinjaPaymentFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusUnprocessableEntity, `{"message":"invalid"}`))

	if err := outbound.NinjaPayment(context.Background(), to, "C1", "I1", 1, "2026-09-01", ""); err == nil {
		t.Fatal("expected error on 422")
	}
}

func TestNinjaDraftInvoiceSendsLinesAndReturnsNumber(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"data":{"id":"I9","number":"2026-0042"}}`))
	lines := []outbound.NinjaLine{
		{Product: "Consulting", Notes: "September", Quantity: 7.5, Cost: 95},
		{Product: "Travel", Notes: "", Quantity: 1, Cost: 30},
	}

	number, err := outbound.NinjaDraftInvoice(context.Background(), to, "C1", lines)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if number != "2026-0042" {
		t.Fatalf("number = %q", number)
	}

	c := f.one(t)
	assertNinja(t, c, http.MethodPost, "/api/v1/invoices")
	want := roundTrip(t, map[string]any{"client_id": "C1", "line_items": []any{
		map[string]any{"product_key": "Consulting", "notes": "September", "quantity": 7.5, "cost": 95},
		map[string]any{"product_key": "Travel", "notes": "", "quantity": 1, "cost": 30},
	}})
	if got := c.json(t); !reflect.DeepEqual(any(got), want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
}

func TestNinjaDraftInvoiceFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusForbidden, `{}`))

	number, err := outbound.NinjaDraftInvoice(context.Background(), to, "C1", nil)
	if err == nil || number != "" {
		t.Fatalf("expected error and no number, got %q, %v", number, err)
	}
}

func TestNinjaExpenseCreateReturnsKeyAndNumber(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"data":{"id":"E7","number":"0815"}}`))
	body := map[string]any{"amount": 42.5, "date": "2026-08-01", "vendor_id": "Kx9"}

	key, number, err := outbound.NinjaExpenseCreate(context.Background(), to, body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if key != "E7" || number != "0815" {
		t.Fatalf("got key %q number %q", key, number)
	}

	c := f.one(t)
	assertNinja(t, c, http.MethodPost, "/api/v1/expenses")
	if got := c.json(t); !reflect.DeepEqual(any(got), roundTrip(t, body)) {
		t.Fatalf("body = %v", got)
	}
}

func TestNinjaExpenseCreateFailsWithoutIDInAnswer(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"data":{}}`))

	if _, _, err := outbound.NinjaExpenseCreate(context.Background(), to, map[string]any{}); err == nil {
		t.Fatal("expected error when the answer has no id")
	}
}

func TestNinjaExpenseCreateFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusInternalServerError, `{}`))

	if _, _, err := outbound.NinjaExpenseCreate(context.Background(), to, map[string]any{}); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestNinjaExpenseSetPutsOnlyGivenValues(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"data":{"id":"E7"}}`))
	values := map[string]string{"custom_value2": "https://docs/documents/44/"}

	if err := outbound.NinjaExpenseSet(context.Background(), to, "E7", values); err != nil {
		t.Fatalf("set: %v", err)
	}

	c := f.one(t)
	assertNinja(t, c, http.MethodPut, "/api/v1/expenses/E7")
	if got := c.json(t); !reflect.DeepEqual(any(got), roundTrip(t, values)) {
		t.Fatalf("body = %v", got)
	}
}

func TestNinjaExpenseSetFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusNotFound, `{}`))

	if err := outbound.NinjaExpenseSet(context.Background(), to, "E7", map[string]string{"x": "y"}); err == nil {
		t.Fatal("expected error on 404")
	}
}

func TestPaperlessUploadSendsMultipartAndReturnsTask(t *testing.T) {
	var got struct {
		title, name, content, auth string
		err                        error
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/documents/post_document/" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		got.auth = r.Header.Get("Authorization")
		got.title = r.FormValue("title")
		file, header, err := r.FormFile("document")
		if err != nil {
			got.err = err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		raw, _ := io.ReadAll(file)
		got.name, got.content = header.Filename, string(raw)
		_, _ = io.WriteString(w, `"4f1c-task"`)
	}))
	defer srv.Close()
	to := outbound.Target{URL: srv.URL + "/", Token: token, VerifyTLS: true}

	task, err := outbound.PaperlessUpload(context.Background(), to, "receipt.pdf", "Hotel", []byte("%PDF-1.7"))
	if err != nil || got.err != nil {
		t.Fatalf("upload: %v / %v", err, got.err)
	}
	if task != "4f1c-task" {
		t.Fatalf("task = %q", task)
	}
	if got.auth != "Token "+token || got.title != "Hotel" || got.name != "receipt.pdf" || got.content != "%PDF-1.7" {
		t.Fatalf("unexpected upload: %+v", got)
	}
}

func TestPaperlessUploadFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusUnauthorized, `{}`))

	if _, err := outbound.PaperlessUpload(context.Background(), to, "a.pdf", "", []byte("x")); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestPaperlessFieldsSetUsesBulkEdit(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `"OK"`))

	if err := outbound.PaperlessFieldsSet(context.Background(), to, 44, map[int64]any{3: "E7", 5: 12.5}); err != nil {
		t.Fatalf("fields: %v", err)
	}

	c := f.one(t)
	if c.Method != http.MethodPost || c.Path != "/api/documents/bulk_edit/" {
		t.Fatalf("unexpected %s %s", c.Method, c.Path)
	}
	if got := c.Header.Get("Authorization"); got != "Token "+token {
		t.Fatalf("Authorization = %q", got)
	}
	want := roundTrip(t, map[string]any{
		"documents": []int64{44}, "method": "modify_custom_fields",
		"parameters": map[string]any{"add_custom_fields": map[string]any{"3": "E7", "5": 12.5}, "remove_custom_fields": []int64{}},
	})
	if got := c.json(t); !reflect.DeepEqual(any(got), want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
}

// Older Paperless versions lack bulk_edit's modify_custom_fields: the
// write falls back to PATCH with existing fields merged in.
func TestPaperlessFieldsSetFallsBackToMergedPatch(t *testing.T) {
	var f fake
	var patch call
	to := f.serve(t, func(w http.ResponseWriter, c call) {
		switch {
		case c.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
		case c.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"id":44,"custom_fields":[{"field":3,"value":"old"},{"field":9,"value":"keep"}]}`)
		case c.Method == http.MethodPatch:
			patch = c
			_, _ = io.WriteString(w, `{"id":44}`)
		}
	})

	if err := outbound.PaperlessFieldsSet(context.Background(), to, 44, map[int64]any{3: "new"}); err != nil {
		t.Fatalf("fields: %v", err)
	}

	if patch.Path != "/api/documents/44/" {
		t.Fatalf("PATCH path = %q", patch.Path)
	}
	if got := patch.Header.Get("Authorization"); got != "Token "+token {
		t.Fatalf("Authorization = %q", got)
	}
	fields, _ := patch.json(t)["custom_fields"].([]any)
	merged := map[float64]any{}
	for _, raw := range fields {
		m, _ := raw.(map[string]any)
		merged[m["field"].(float64)] = m["value"]
	}
	if !reflect.DeepEqual(merged, map[float64]any{3: "new", 9: "keep"}) {
		t.Fatalf("merged fields = %v", merged)
	}
}

func TestPaperlessFieldsSetFailsWhenDocumentUnreadable(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusNotFound, `{}`))

	if err := outbound.PaperlessFieldsSet(context.Background(), to, 44, map[int64]any{3: "x"}); err == nil {
		t.Fatal("expected error when bulk_edit and GET fail")
	}
}

func TestKimaiMarkExportedPatchesExportFlag(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusOK, `{"id":17,"exported":true}`))

	if err := outbound.KimaiMarkExported(context.Background(), to, 17); err != nil {
		t.Fatalf("mark: %v", err)
	}

	c := f.one(t)
	if c.Method != http.MethodPatch || c.Path != "/api/timesheets/17/export" {
		t.Fatalf("unexpected %s %s", c.Method, c.Path)
	}
	if got := c.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q", got)
	}
	if len(c.Body) != 0 {
		t.Fatalf("expected no body, got %q", c.Body)
	}
}

func TestKimaiMarkExportedFailsOnServerRejection(t *testing.T) {
	var f fake
	to := f.serve(t, answer(http.StatusForbidden, `{}`))

	if err := outbound.KimaiMarkExported(context.Background(), to, 17); err == nil {
		t.Fatal("expected error on 403")
	}
}
