package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestPaymentMatchesCheckName: the amount matches only the open balance;
// a payer whose name is not the invoice's client is marked, and a
// payment of a total whose invoice is nearly paid is no match at all.
func TestPaymentMatchesCheckName(t *testing.T) {
	today := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	ninja := &sources.NinjaDataset{
		Clients: []sources.NinjaClient{{ID: 1, Name: "Nivre Film & Studio GmbH"}, {ID: 2, Name: "Musealis GmbH"}},
		Invoices: []sources.NinjaInvoice{
			{ID: 38, Number: "R2025/0038", ClientID: 1, Status: "sent", Date: "2026-06-01", Amount: 952, Balance: 952},
			{ID: 4, Number: "R2026/0004", ClientID: 2, Status: "partial", Date: "2026-06-01", Amount: 535.5, Balance: 0.5},
			{ID: 41, Number: "R2026/0041", ClientID: 2, Status: "sent", Date: "2026-08-01", Amount: 100, Balance: 100},
		},
	}
	sure := &sources.SureDataset{Transactions: []sources.SureTxn{
		{ID: "a", Date: "2026-07-22", Name: "musealis GmbH", Amount: 952},
		{ID: "b", Date: "2026-07-24", Name: "Very Media GmbH", Amount: 535.5},
		{ID: "c", Date: "2026-09-01", Name: "MUSEALIS GMBH Rechnung", Amount: 100},
	}}

	got := map[string]metrics.PaymentMatch{}
	for _, m := range metrics.PaymentMatches(sure, ninja, today, 120, nil) {
		got[m.Txn.ID] = m
	}
	if m, ok := got["a"]; !ok || m.Invoice.ID != 38 || m.NameFits {
		t.Errorf("a: %+v", m)
	}
	if m, ok := got["b"]; ok {
		t.Errorf("b paid a total of a nearly paid invoice, matched %+v", m)
	}
	if m, ok := got["c"]; !ok || m.Invoice.ID != 41 || !m.NameFits {
		t.Errorf("c: %+v", m)
	}
}

// A payer whose name says nothing ("NSH Treuhand" pays for Nivre) fits
// once the Verbund links it to the client; its other incomes count as
// the client's, while a payer linked to "no client" stays anybody's.
func TestPaymentMatchesFollowPayerLinks(t *testing.T) {
	today := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	ninja := &sources.NinjaDataset{
		Clients:  []sources.NinjaClient{{ID: 1, Key: "Nv1", Name: "Nivre Film & Studio GmbH"}},
		Invoices: []sources.NinjaInvoice{{ID: 38, Number: "R2025/0038", ClientID: 1, Status: "sent", Date: "2026-06-01", Amount: 952, Balance: 952}},
	}
	sure := &sources.SureDataset{Transactions: []sources.SureTxn{
		{ID: "a", Date: "2026-07-22", Name: "Gutschrift", Merchant: "NSH Treuhand", Amount: 952},
		{ID: "b", Date: "2026-08-22", Name: "Gutschrift", Merchant: "NSH Treuhand", Amount: 120},
		{ID: "c", Date: "2026-08-23", Name: "Erstattung", Merchant: "Finanzamt", Amount: 80},
	}}

	if m := metrics.PaymentMatches(sure, ninja, today, 120, nil); len(m) != 1 || m[0].NameFits {
		t.Fatalf("unlinked: %+v", m)
	}
	payers := metrics.PayerMap{"NSH Treuhand": "Nv1", "Finanzamt": ""}
	matches := metrics.PaymentMatches(sure, ninja, today, 120, payers)
	if len(matches) != 1 || !matches[0].NameFits || !matches[0].Sure() {
		t.Fatalf("linked: %+v", matches)
	}
	unmatched := metrics.UnmatchedIncome(sure, ninja, matches, today, 120, payers)
	if len(unmatched) != 1 || unmatched[0].ID != "b" {
		t.Fatalf("unmatched: %+v", unmatched)
	}
}

// Without a merchant the booking text names the payer; invoice numbers
// and dates in it vary per booking and are left out.
func TestPayer(t *testing.T) {
	for _, c := range []struct {
		txn  sources.SureTxn
		want string
	}{
		{sources.SureTxn{Name: "Gutschrift", Merchant: "ACME GmbH"}, "ACME GmbH"},
		{sources.SureTxn{Name: "Northlight Pictures RE-2026-017"}, "Northlight Pictures"},
		{sources.SureTxn{Name: "Northlight Pictures  RE-2026-021 vom 01.09."}, "Northlight Pictures vom"},
		{sources.SureTxn{Name: "4711"}, "4711"},
	} {
		if got := metrics.Payer(c.txn); got != c.want {
			t.Errorf("%+v: %q, want %q", c.txn, got, c.want)
		}
	}
}
