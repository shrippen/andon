package vatcheck

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/outbound"
	"andon/internal/services/receipts"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// An Amazon order: one debit of 84,37, three invoices.
//
//	DE5 Amazon EU     39,99  scan 1, names the order
//	KP  Kabelprofi24  14,49  expense x1 (scan first), unlinked
//	RE  Buchhaus      29,89  scan 3, names the order
//	    Drucker       22,90  scan 4, another order
const order = "302-4419871-5520347"

func splitBook() book {
	return book{num: enums.NotationDE, ninja: &sources.NinjaDataset{
		BankTxns: []sources.NinjaBankTxn{{Key: "b1", Date: "2026-10-16", Payer: "Amazon Payments", Description: "AMAZON PAYMENTS " + order,
			Amount: 84.37, Debit: true, Status: sources.BankOpen}},
		Expenses: []sources.NinjaExpense{{Key: "x1", Number: "EX-63", VendorKey: "v2", Date: "2026-10-17", Amount: 14.49},
			{Key: "x2", Number: "EX-50", VendorKey: "v2", Date: "2026-10-01", Amount: 14.49, TxnKey: "b0"}},
		Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Amazon EU S.à r.l."}, {Key: "v2", Name: "Kabelprofi24 GmbH"}},
	}}
}

func scan(id int64, day, title, content string, amount float64) receipts.Candidate {
	return receipts.Candidate{Doc: sources.ReceiptDoc{ID: id, Day: day, Title: title, Content: content}, Amount: amount}
}

// TestSplitOffers: the order's scans come first and ticked, then the
// rest near the booking; an expense found is a part too; the order's
// parts plus the expense make the booking, but only hits count for the
// proposal.
func TestSplitOffers(t *testing.T) {
	b := splitBook()
	pool := []receipts.Candidate{
		scan(4, "2026-10-14", "Rechnung Drucker", "Bestellung 302-1111111-2222222", 22.90),
		scan(1, "2026-10-16", "Rechnung Amazon EU", "Bestellnr. "+order, 39.99),
		scan(3, "2026-10-18", "Rechnung Buchhaus", "Ihre Bestellung "+order, 29.89),
		scan(5, "2026-09-01", "Alt", order, 10),        // too far
		scan(6, "2026-10-16", "Zu groß", order, 84.37), // the whole booking: no part
	}
	c := Case{BankCase: metrics.BankCase{Txn: b.ninja.BankTxns[0], Action: metrics.ActionCreate}}
	b.splitOf(&c, pool, nil)

	if len(c.Split) != maxParts {
		t.Fatalf("slots: %d", len(c.Split))
	}
	var got []string
	for _, p := range c.Split {
		switch {
		case p.Scan != nil:
			got = append(got, p.Scan.Doc.Title+map[bool]string{true: " ✓", false: ""}[p.Order])
		case p.Expense != nil:
			got = append(got, p.Expense.Number)
		}
	}
	want := []string{"Rechnung Amazon EU ✓", "Rechnung Buchhaus ✓", "EX-63", "Rechnung Drucker"}
	if !slices.Equal(got, want) {
		t.Fatalf("offers %v, want %v", got, want)
	}
	if c.Action != metrics.ActionCreate {
		t.Fatalf("hits 69,88 of 84,37 proposed %q", c.Action)
	}

	// Hits that make up the booking propose the split.
	pool[0] = scan(4, "2026-10-14", "Rechnung Kabelprofi24", "Bestellung "+order, 14.49)
	c = Case{BankCase: metrics.BankCase{Txn: b.ninja.BankTxns[0], Action: metrics.ActionCreate}}
	b.splitOf(&c, pool, nil)
	if c.Action != ActionSplit {
		t.Fatalf("proposed %q, want split", c.Action)
	}
}

// TestOrderNumbers: an order number, not a date or a short reference.
func TestOrderNumbers(t *testing.T) {
	got := orderNumbers("AMAZON 302-4419871-5520347 vom 2026-10-16 Ref 4711")
	if !slices.Equal(got, []string{order}) {
		t.Fatalf("got %v", got)
	}
}

func splitStep(slots ...map[string]string) workbench.Step {
	s := workbench.Step{Case: "b1", Action: string(ActionSplit), Fields: map[string]string{}}
	for i, slot := range slots {
		for k, v := range slot {
			s.Fields[PartField(i, k)] = v
		}
	}
	return s
}

// TestSplitCheck: the parts must make the booking; a found expense
// brings its amount; one taken or used twice, a sum off by a cent or a
// single part is refused.
func TestSplitCheck(t *testing.T) {
	b := splitBook()
	amazon := map[string]string{FieldUse: UseExpense, FieldVendor: "Amazon EU S.à r.l.", FieldAmount: "39,99", FieldTax: "19", FieldScan: "1", FieldDay: "2026-10-16"}
	kabel := map[string]string{FieldUse: UseExpense, FieldExpense: "x1", FieldAmount: "1,00"}
	book := map[string]string{FieldUse: UseExpense, FieldVendor: "Buchhaus Nordlicht", FieldAmount: "29,89", FieldTax: "7", FieldScan: "3"}
	off := map[string]string{FieldVendor: "unused", FieldAmount: "5"}

	s := splitStep(amazon, off, kabel, book)
	it, problem := b.Check(&s)
	if problem != workbench.ProblemNone {
		t.Fatalf("problem %q", problem)
	}
	if len(it.Parts) != 3 || it.Parts[1].Expense == nil || it.Parts[1].Txn.Amount != 14.49 || it.Parts[2].Tax != 1.96 || it.Parts[0].Txn.Date != "2026-10-16" {
		t.Fatalf("parts %+v", it.Parts)
	}
	if _, ok := s.Fields[PartField(1, FieldVendor)]; ok || s.Fields[PartField(2, FieldAmount)] != "14,49" || s.Fields[PartField(2, FieldDay)] != "2026-10-17" {
		t.Fatalf("fields %v", s.Fields)
	}

	cases := map[string]struct {
		step workbench.Step
		want workbench.Problem
	}{
		"a cent short": {splitStep(amazon, kabel, map[string]string{FieldUse: UseExpense, FieldVendor: "Buchhaus", FieldAmount: "29,88"}), ProblemSplitSum},
		"one part":     {splitStep(map[string]string{FieldUse: UseExpense, FieldVendor: "Amazon", FieldAmount: "84,37"}), ProblemSplitParts},
		"all private": {splitStep(map[string]string{FieldUse: UsePrivate, FieldAmount: "40"},
			map[string]string{FieldUse: UsePrivate, FieldAmount: "44,37"}), ProblemSplitParts},
		"expense twice":  {splitStep(kabel, kabel, map[string]string{FieldUse: UseExpense, FieldVendor: "X", FieldAmount: "55,39"}), ProblemTarget},
		"expense linked": {splitStep(map[string]string{FieldUse: UseExpense, FieldExpense: "x2"}, amazon, book), ProblemTarget},
		"scan twice":     {splitStep(amazon, map[string]string{FieldUse: UseExpense, FieldVendor: "Y", FieldAmount: "44,38", FieldScan: "1"}), ProblemTarget},
		"no vendor":      {splitStep(amazon, map[string]string{FieldUse: UseExpense, FieldAmount: "44,38"}), ProblemNoVendor},
		"no amount":      {splitStep(amazon, map[string]string{FieldUse: UseExpense, FieldVendor: "Y"}), ProblemAmount},
	}
	for name, c := range cases {
		if _, problem := b.Check(&c.step); problem != c.want {
			t.Errorf("%s: problem %q, want %q", name, problem, c.want)
		}
	}

	// A private part counts to the sum and becomes no expense.
	s = splitStep(amazon, map[string]string{FieldUse: UsePrivate, FieldAmount: "44,38", FieldNote: "Geschenk"})
	if it, problem := b.Check(&s); problem != workbench.ProblemNone || !it.Parts[1].Private {
		t.Fatalf("private: %q %+v", problem, it.Parts)
	}
}

// TestSplitWrite: new expenses first (a new vendor once for two parts),
// dated by their invoice and paid on the booking's day, then one link
// of all to the booking.
func TestSplitWrite(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	var bodies []map[string]any
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		seen, bodies = append(seen, path), append(bodies, body)
		switch path {
		case "vendors":
			rw.Write([]byte(`{"data": {"id": "v9"}}`))
		case "expenses":
			n++
			rw.Write([]byte(`{"data": {"id": "n` + string(rune('0'+n)) + `", "number": "EX-9` + string(rune('0'+n)) + `"}}`))
		case "bank_transactions/match":
			rw.Write([]byte(`{"data": [{"id": "b1", "status_id": "3", "expense_id": "n1,x1,n2"}]}`))
		}
	}))
	t.Cleanup(srv.Close)

	b := splitBook()
	w := &writer{book: b, to: outbound.Target{URL: srv.URL, Token: "n", VerifyTLS: true}}
	part := func(vendor, day string, amount float64) Item {
		return Item{Txn: sources.NinjaBankTxn{Key: "b1", Date: day, Amount: amount, Debit: true}, Vendor: vendor, Gross: true, Paid: true}
	}
	p := Planned{Step: workbench.Step{Case: "b1", Action: string(ActionSplit)}, Item: Item{Txn: b.ninja.BankTxns[0],
		Parts: []Item{part("Buchhaus", "2026-10-18", 29.89), {Expense: &b.ninja.Expenses[0]}, part("Buchhaus", "2026-10-19", 30), {Private: true}}}}
	target, err := w.Write(t.Context(), p)
	if err != nil || target != "EX-91, EX-63, EX-92" {
		t.Fatalf("target %q err %v", target, err)
	}
	if want := []string{"vendors", "expenses", "expenses", "bank_transactions/match"}; !slices.Equal(seen, want) {
		t.Fatalf("calls %v, want %v", seen, want)
	}
	if bodies[1]["date"] != "2026-10-18" || bodies[1]["payment_date"] != "2026-10-16" || bodies[1]["vendor_id"] != "v9" || bodies[2]["vendor_id"] != "v9" {
		t.Fatalf("expense %v", bodies[1])
	}
	list, _ := bodies[3]["transactions"].([]any)
	if item, _ := list[0].(map[string]any); item["expense_id"] != "n1,x1,n2" {
		t.Fatalf("match %v", bodies[3])
	}
}
