package receipts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"andon/internal/outbound"
	"andon/internal/sources"
)

// PaperNinja's linking cases, ported: both sides written, the invoice
// number carried over, a failed scan side rolled back.

type written struct {
	mu        sync.Mutex
	expense   []map[string]any // PUT bodies
	docFields map[string][]map[string]any
}

// fakes serves Invoice Ninja (PUT /api/v1/expenses/{id}) and Paperless
// (bulk_edit, GET/PATCH fallback); scans listed in fail answer 500.
func fakes(t *testing.T, fail ...string) (*written, outbound.Target, outbound.Target) {
	t.Helper()
	w := &written{docFields: map[string][]map[string]any{}}
	ninja := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.mu.Lock()
		w.expense = append(w.expense, body)
		w.mu.Unlock()
		rw.Write([]byte(`{"data": {}}`))
	}))
	docs := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Documents  []int64
			Parameters struct {
				Add map[string]any `json:"add_custom_fields"`
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// bulk_edit names the scan in its body, the PATCH fallback in the path.
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/documents/"), "/")
		if len(body.Documents) == 1 {
			id = strconv.FormatInt(body.Documents[0], 10)
		}
		for _, f := range fail {
			if f == id {
				rw.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.mu.Lock()
		w.docFields[id] = append(w.docFields[id], body.Parameters.Add)
		w.mu.Unlock()
		rw.Write([]byte(`"OK"`))
	}))
	t.Cleanup(ninja.Close)
	t.Cleanup(docs.Close)
	return w, outbound.Target{URL: ninja.URL, Token: "n", VerifyTLS: true}, outbound.Target{URL: docs.URL, Token: "p", VerifyTLS: true}
}

func testWriter(ninja, docs outbound.Target, e sources.ReceiptExpense, scans ...sources.ReceiptDoc) writer {
	return writer{Links: Links{NinjaURL: "https://in.example", DocsURL: "https://pl.example"}, mapping: mapped,
		ninja: ninja, docsTo: docs, expense: e, docs: scans}
}

func TestLinkWritesBothSides(t *testing.T) {
	w, ninja, docs := fakes(t)
	e := expense(func(e *sources.ReceiptExpense) { e.Key, e.Custom[0] = "exp-1", "RE-9" })
	if err := testWriter(ninja, docs, e, doc(func(d *sources.ReceiptDoc) { d.ID = 44 })).link(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.expense[0]["custom_value2"] != "https://pl.example/documents/44/" || w.expense[0]["custom_value1"] != "RE-9" {
		t.Fatalf("expense: %v", w.expense)
	}
	fields := w.docFields["44"][0]
	if fields["2"] != "EX-001" || fields["3"] != "https://in.example/expenses/exp-1/edit" || fields["1"] != "RE-9" {
		t.Fatalf("scan: %v", fields)
	}
}

func TestLinkTakesInvoiceNumberFromScan(t *testing.T) {
	w, ninja, docs := fakes(t)
	scan := doc(func(d *sources.ReceiptDoc) { d.Custom[1] = "INV-77" })
	if err := testWriter(ninja, docs, expense(nil), scan).link(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.expense[0]["custom_value1"] != "INV-77" {
		t.Fatalf("expense: %v", w.expense)
	}
}

func TestLinkRollsBackWhenScanFails(t *testing.T) {
	w, ninja, docs := fakes(t, "10")
	if err := testWriter(ninja, docs, expense(nil), doc(nil)).link(t.Context()); err == nil {
		t.Fatal("want an error")
	}
	if len(w.expense) != 2 || !strings.HasSuffix(w.expense[0]["custom_value2"].(string), "/documents/10/") || w.expense[1]["custom_value2"] != "" {
		t.Fatalf("expense: %v", w.expense)
	}
}

func TestLinkComboJoinsURLsAndRollsBack(t *testing.T) {
	w, ninja, docs := fakes(t, "12")
	scans := []sources.ReceiptDoc{doc(func(d *sources.ReceiptDoc) { d.ID = 11 }), doc(func(d *sources.ReceiptDoc) { d.ID = 12 })}
	if err := testWriter(ninja, docs, expense(nil), scans...).link(t.Context()); err == nil {
		t.Fatal("want an error")
	}
	if w.expense[0]["custom_value2"] != "https://pl.example/documents/11/ https://pl.example/documents/12/" || w.expense[0]["custom_value1"] != nil {
		t.Fatalf("expense: %v", w.expense[0])
	}
	if last := w.expense[len(w.expense)-1]; last["custom_value2"] != "" {
		t.Fatalf("not rolled back: %v", last)
	}
	if cleared := w.docFields["11"]; len(cleared) != 2 || cleared[1]["2"] != "" {
		t.Fatalf("scan 11: %v", cleared)
	}
}

func TestUnlinkKeepsOtherScans(t *testing.T) {
	w, ninja, docs := fakes(t)
	e := expense(func(e *sources.ReceiptExpense) {
		e.Custom[1] = "https://pl.example/documents/11/ https://pl.example/documents/12/"
	})
	if err := testWriter(ninja, docs, e).unlink(t.Context(), 11); err != nil {
		t.Fatal(err)
	}
	if w.expense[0]["custom_value2"] != "https://pl.example/documents/12/" {
		t.Fatalf("expense: %v", w.expense)
	}
	if f := w.docFields["11"][0]; f["2"] != "" || f["3"] != "" {
		t.Fatalf("scan: %v", f)
	}
}

func TestDocIDs(t *testing.T) {
	if got := docIDs("https://pl/documents/11/ https://pl/documents/12/details"); len(got) != 2 || got[1] != 12 {
		t.Fatalf("ids: %v", got)
	}
}

func TestSuggestMapping(t *testing.T) {
	slots := []Slot{{1, "Rechnungsnummer"}, {2, "Paperless"}, {3, ""}}
	fields := []sources.DocField{{ID: 1, Name: "Rechnungsnummer", Type: "string"}, {ID: 2, Name: "Ausgabe", Type: "string"},
		{ID: 3, Name: "Invoice Ninja", Type: "url"}, {ID: 4, Name: "Betrag", Type: "monetary"}}
	want := Mapping{InvoiceSlot: 1, LinkSlot: 2, FieldInvoice: 1, FieldExpense: 2, FieldLink: 3, FieldAmount: 4}
	if got := suggest(slots, fields); got != want {
		t.Fatalf("suggest: %+v", got)
	}
}
