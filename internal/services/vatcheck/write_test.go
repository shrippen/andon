package vatcheck

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/outbound"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// TestWriteReceiptNoBooking: a receipt's new expense links no booking
// (there is none: "d-<id>" is a scan); a booking's new expense does.
func TestWriteReceiptNoBooking(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		rw.Write([]byte(`{"data": {"id": "e9", "number": "EX-9"}}`))
	}))
	t.Cleanup(srv.Close)
	w := &writer{book: book{ninja: &sources.NinjaDataset{Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Copyshop"}}}},
		to: outbound.Target{URL: srv.URL, Token: "n", VerifyTLS: true}}
	write := func(act metrics.BankAction, key string) []string {
		paths = nil
		p := Planned{Step: workbench.Step{Case: key, Action: string(act)},
			Item: Item{Txn: sources.NinjaBankTxn{Key: key, Date: "2026-10-20", Amount: 18.4, Debit: true}, Vendor: "Copyshop", Gross: true}}
		if _, err := w.Write(t.Context(), p); err != nil && act == ActionReceipt {
			t.Fatalf("%s: %v", act, err) // the fake does not answer a match as booked
		}
		return paths
	}

	linked := func(paths []string) bool {
		return slices.ContainsFunc(paths, func(p string) bool { return p == "POST /api/v1/bank_transactions/match" })
	}
	if got := write(ActionReceipt, "d-207"); linked(got) || !slices.Contains(got, "POST /api/v1/expenses") {
		t.Fatalf("receipt: %v", got)
	}
	if got := write(metrics.ActionCreate, "b1"); !linked(got) {
		t.Fatalf("booking: %v", got)
	}
}

// TestReceiptBookedMeanwhile: an open debit that fits the receipt (one
// imported after the page opened, or one the scan had no amount for)
// takes it; the step says to link it there.
func TestReceiptBookedMeanwhile(t *testing.T) {
	// The amount reads in the user's notation: "72,14" or "72.14".
	for n, typed := range map[enums.Notation]string{enums.NotationDE: "72,14", enums.NotationEN: "72.14"} {
		b := book{num: n, ninja: &sources.NinjaDataset{BankTxns: []sources.NinjaBankTxn{
			{Key: "b9", Date: "2026-10-02", Amount: 72.14, Debit: true, Status: sources.BankOpen}}}}
		s := workbench.Step{Case: "d-207", Action: string(ActionReceipt), Fields: map[string]string{
			FieldVendor: "Tankstelle", FieldAmount: typed, FieldDay: "2026-09-30"}}
		if _, problem := b.receipted(&s, "207"); problem != ProblemDebit {
			t.Fatalf("%s %q: problem %q", n, typed, problem)
		}
	}
}
