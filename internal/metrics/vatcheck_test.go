package metrics_test

import (
	"slices"
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Accounts of the test: the business one, and the private one that pays
// the train ticket.
const (
	business = "Geschäftskonto"
	private  = "Privatkonto"
)

// vatBook is September 2026: known vendors, one booked transaction, one
// archived, one open transaction of each kind, and three on the private
// account.
func vatBook() (*sources.NinjaDataset, *sources.BankData) {
	ninja := &sources.NinjaDataset{
		Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Kabelwerk Studiobedarf"}, {Key: "v2", Name: "Elbnetz Mobilfunk"},
			{Key: "v3", Name: "Mietwagen Nord"}, // "nord" is in "Nordhost", but not a word of it
			{Key: "v4", Name: "DB Fernverkehr"}},
		Categories: []sources.NinjaCategory{{Key: "c1", Name: "Technik"}, {Key: "c2", Name: "Hosting"}},
		Clients:    []sources.NinjaClient{{ID: 1, Name: "Northlight Pictures"}},
		Invoices:   []sources.NinjaInvoice{{ID: 7, Key: "i7", Number: "R-2026-017", ClientID: 1, Status: "sent", Amount: 2380, Balance: 2380}},
		Payments:   []sources.NinjaPayment{{Key: "p1", Number: "Z-0001", ClientID: 1, Date: "2026-09-17", Amount: 500}},
		Expenses: []sources.NinjaExpense{
			{Key: "e1", VendorKey: "v1", CategoryKey: "c1", Date: "2026-08-20", Amount: 89.7, Tax: 14.32, TaxName: "MwSt.", TaxRate: 19, TxnKey: "b0"},
			{Key: "e2", VendorKey: "v2", Date: "2026-09-02", Amount: 39.95, Tax: 6.38},
			{Key: "e3", VendorKey: "v2", Date: "2026-09-12", Amount: 12.5},
			{Key: "e4", VendorKey: "v4", CategoryKey: "c3", Date: "2026-08-01", Amount: 58, Tax: 3.79, TaxName: "MwSt. 7 %", TaxRate: 7, TxnKey: "p0"},
		},
		BankTxns: []sources.NinjaBankTxn{
			{Key: "b0", Date: "2026-08-21", Payer: "Kabelwerk Studiobedarf", Amount: 89.7, Debit: true, Status: sources.BankDone, ExpenseKeys: []string{"e1"}, Account: business, Role: sources.RoleBusiness},
			{Key: "b1", Date: "2026-09-03", Payer: "Elbnetz Mobilfunk", Amount: 39.95, Debit: true, Status: sources.BankOpen, Account: business, Role: sources.RoleBusiness},
			{Key: "b2", Date: "2026-09-08", Payer: "Kabelwerk Studiobedarf", Description: "KW-56107", Amount: 899, Debit: true, Status: sources.BankOpen, Account: business, Role: sources.RoleBusiness},
			{Key: "b3", Date: "2026-09-10", Payer: "NH Billing", Description: "Rechnung 09", Amount: 41.65, Debit: true, Status: sources.BankOpen, Account: business, Role: sources.RoleBusiness},
			{Key: "b4", Date: "2026-09-15", Payer: "Northlight", Description: "RE R-2026-017", Amount: 2380, Status: sources.BankOpen, Account: business, Role: sources.RoleBusiness},
			{Key: "b5", Date: "2026-09-20", Payer: "Wellenklang", Amount: 10.99, Debit: true, Status: sources.BankOpen, Archived: true, Account: business, Role: sources.RoleBusiness},
			{Key: "b6", Date: "2026-09-01", Payer: "DB Fernverkehr AG", Description: "Deutschlandticket", Amount: 58, Debit: true, Status: sources.BankOpen, Account: private, Role: sources.RolePrivate},
			{Key: "b7", Date: "2026-09-02", Payer: "Supermarkt", Amount: 64.2, Debit: true, Status: sources.BankOpen, Account: private, Role: sources.RolePrivate},
			{Key: "b8", Date: "2026-09-02", Payer: "Arbeitgeber", Amount: 900, Status: sources.BankOpen, Account: private, Role: sources.RolePrivate},
			{Key: "b9", Date: "2026-09-18", Payer: "Northlight Pictures", Amount: 500, Status: sources.BankOpen, Account: business, Role: sources.RoleBusiness},
		},
	}
	sure := &sources.BankData{Accounts: []sources.BankAccount{{Name: business, Role: sources.RoleBusiness}, {Name: private, Role: sources.RolePrivate}},
		Transactions: []sources.BankTxn{
			{ID: "s1", Date: "2026-09-08", Name: "Kabelwerk", Amount: -899, Account: business, Role: sources.RoleBusiness},
			{ID: "s2", Date: "2026-09-11", Name: "NH BILLING", Merchant: "Nordhost Server", Category: "Hosting", Amount: -41.65, Account: business, Role: sources.RoleBusiness},
			{ID: "s3", Date: "2026-09-25", Name: "Druckerei", Amount: -58, Account: business, Role: sources.RoleBusiness},
			{ID: "s4", Date: "2026-09-26", Name: "Bäckerei", Amount: -6.4, Account: private, Role: sources.RolePrivate},
		}}
	return ninja, sure
}

func september() metrics.VATInputs {
	ninja, sure := vatBook()
	return metrics.VATInputs{Ninja: ninja, Sure: sure, Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Method: "soll",
		Today: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}
}

func TestBankCases(t *testing.T) {
	cases := metrics.BankCases(september())
	byKey := map[string]metrics.BankCase{}
	for _, c := range cases {
		byKey[c.Txn.Key] = c
	}
	if len(cases) != 7 || byKey["b0"].Txn.Key != "" || byKey["b5"].Txn.Key != "" || byKey["b8"].Txn.Key != "" {
		t.Fatalf("booked, archived and private incomes must drop out: %+v", cases)
	}

	// An income whose payment Ninja has already.
	if c := byKey["b9"]; c.Action != metrics.ActionPayment || c.Payment == nil || c.Payment.Key != "p1" {
		t.Errorf("b9: %+v", c)
	}

	// Paid privately: a vendor with expenses is proposed, the rest waits.
	if c := byKey["b6"]; !c.Private || c.Action != metrics.ActionCreate || c.Draft.TaxRate != 7 || !c.Waiting() {
		t.Errorf("b6 train ticket: %+v", c)
	}
	if c := byKey["b7"]; !c.Private || c.Action != metrics.ActionNone || c.Waiting() {
		t.Errorf("b7 groceries: %+v", c)
	}

	// An expense of the same amount near the booking.
	if c := byKey["b1"]; c.Action != metrics.ActionLink || c.Expenses[0].Key != "e2" {
		t.Errorf("b1: want link to e2, got %+v", c)
	}

	// A known vendor brings its last category and tax.
	c := byKey["b2"]
	if c.Action != metrics.ActionCreate || c.Draft.VendorKey != "v1" || c.Draft.CategoryKey != "c1" || c.Draft.TaxRate != 19 || c.Draft.From != metrics.FromHistory {
		t.Errorf("b2: want draft from history, got %+v", c.Draft)
	}
	if c.Sure == nil || c.Sure.ID != "s1" {
		t.Errorf("b2: want Sure twin s1, got %+v", c.Sure)
	}

	// An unknown payee: Sure's merchant and category, tax unknown.
	c = byKey["b3"]
	if c.Draft.Vendor != "Nordhost Server" || c.Draft.VendorKey != "" || c.Draft.CategoryKey != "c2" || c.Draft.TaxKnown || c.Draft.From != metrics.FromSure {
		t.Errorf("b3: want draft from Sure, got %+v", c.Draft)
	}

	// An income naming the invoice's number.
	if c := byKey["b4"]; c.Action != metrics.ActionInvoice || c.Invoice.Key != "i7" || !c.ByNumber {
		t.Errorf("b4: want invoice i7 by number, got %+v", c)
	}
}

func TestVATCheck(t *testing.T) {
	in := september()
	check := metrics.VATCheckOf(in, metrics.BankCases(in))
	if check.Open != 6 || check.Proposed != 6 {
		t.Errorf("open %d proposed %d, want 6 and 6", check.Open, check.Proposed)
	}

	// The feed ends on 20 Sep, the period on 30 Sep, which is past.
	if !check.FeedLate || check.FeedTo != "2026-09-20" {
		t.Errorf("feed: %s late %v", check.FeedTo, check.FeedLate)
	}

	// e3 lacks tax, while Elbnetz's other expense has it.
	if len(check.NoTax) != 1 || check.NoTax[0].Key != "e3" {
		t.Errorf("no tax: %+v", check.NoTax)
	}

	// The printer's bill is missing in the feed; the bakery is private.
	if len(check.SureOnly) != 1 || check.SureOnly[0].ID != "s3" {
		t.Errorf("sure only: %+v", check.SureOnly)
	}
	if check.Ready() {
		t.Error("a check with open items is not ready")
	}
}

func TestVATSpans(t *testing.T) {
	today := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	spans := metrics.VATSpans(today, "quarterly", 3)
	if spans[0].Key != "2026-Q4" || spans[1].Key != "2026-Q3" || spans[2].Key != "2026-Q2" {
		t.Fatalf("quarters: %+v", spans)
	}
	back, ok := metrics.VATSpanOf("2026-Q3", "quarterly")
	if !ok || !back.Start.Equal(spans[1].Start) || !back.End.Equal(spans[1].End) {
		t.Errorf("Q3 back: %+v", back)
	}
	months := metrics.VATSpans(today, "monthly", 2)
	if months[1].Key != "2026-09" || months[1].End.Day() != 30 {
		t.Errorf("months: %+v", months)
	}
	for _, bad := range []string{"2026-Q5", "x"} {
		if _, ok := metrics.VATSpanOf(bad, "quarterly"); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
	if _, ok := metrics.VATSpanOf("2026-13", "monthly"); ok {
		t.Error("month 13 must not parse")
	}
}

// With roles named, the receipts rule needs no account names: the
// printer's bill on the business account counts, the bakery does not.
func TestMissingReceiptsByRole(t *testing.T) {
	ninja, sure := vatBook()
	in := metrics.ReceiptInputs{Sure: sure, Ninja: ninja, MinAmount: 5, Window: 10, Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	var names []string
	for _, tx := range metrics.MissingReceipts(in) {
		names = append(names, tx.Name)
	}
	if !slices.Contains(names, "Druckerei") || slices.Contains(names, "Bäckerei") {
		t.Fatalf("missing: %v", names)
	}
}

func TestSharedBookings(t *testing.T) {
	ninja, sure := vatBook()
	for i := range ninja.BankTxns {
		ninja.BankTxns[i].AccountKey = ninja.BankTxns[i].Account
	}
	for i := range sure.Transactions {
		sure.Transactions[i].AccountID = sure.Transactions[i].Account
	}
	got := metrics.SharedBookings(ninja, sure)
	if got[business][business] != 2 || got[private][business] != 0 {
		t.Fatalf("shared: %v", got)
	}
}

// Money moved to an own private account is a transfer, to archive; one
// with a fitting expense stays a booking.
func TestBankCasesTransfer(t *testing.T) {
	n := &sources.NinjaDataset{
		BankTxns: []sources.NinjaBankTxn{
			{Key: "out", Date: "2026-09-10", Payer: "Arian", Amount: 500, Debit: true, Role: sources.RoleBusiness},
			{Key: "in", Date: "2026-09-11", Payer: "Arian", Amount: 500, Role: sources.RolePrivate, Account: "Privat"},
			{Key: "rent", Date: "2026-09-12", Payer: "Vermieter", Amount: 300, Debit: true, Role: sources.RoleBusiness},
			{Key: "host", Date: "2026-09-13", Payer: "Nordhost", Amount: 20, Debit: true, Role: sources.RoleBusiness},
		},
		Expenses: []sources.NinjaExpense{{Key: "e1", Date: "2026-09-13", Amount: 20}},
	}
	s := &sources.BankData{Transactions: []sources.BankTxn{
		{ID: "s1", Date: "2026-09-13", Amount: 300, Role: sources.RolePrivate, Account: "Tagesgeld"},
		{ID: "s2", Date: "2026-09-13", Amount: 20, Role: sources.RolePrivate, Account: "Tagesgeld"},
	}}
	start, _ := metrics.ParseDay("2026-09-01")
	end, _ := metrics.ParseDay("2026-09-30")
	cases := metrics.BankCases(metrics.VATInputs{Ninja: n, Sure: s, Start: start, End: end})
	got := map[string]*metrics.BankCase{}
	for i := range cases {
		got[cases[i].Txn.Key] = &cases[i]
	}
	if c := got["out"]; c.Action != metrics.ActionSkip || c.Transfer == nil || c.Transfer.Account != "Privat" {
		t.Errorf("feed transfer: %+v", c)
	}
	if c := got["rent"]; c.Action != metrics.ActionSkip || c.Transfer == nil || c.Transfer.Account != "Tagesgeld" {
		t.Errorf("Sure transfer: %+v", c)
	}
	if c := got["host"]; c.Action != metrics.ActionLink || c.Transfer != nil {
		t.Errorf("expense stays a booking: %+v", c)
	}
}

// Vendors of one first word group, the busiest first; a vendor whose
// name merely contains the word elsewhere stays apart.
func TestVendorDuplicates(t *testing.T) {
	n := &sources.NinjaDataset{
		Vendors:  []sources.NinjaVendor{{Key: "a", Name: "NORDHOST GmbH"}, {Key: "b", Name: "Nordhost"}, {Key: "c", Name: "Mietwagen Nord"}, {Key: "d", Name: "Kabelwerk"}},
		Expenses: []sources.NinjaExpense{{VendorKey: "b"}, {VendorKey: "b"}, {VendorKey: "a"}},
	}
	g := metrics.VendorDuplicates(n)
	if len(g) != 1 || g[0].Vendors[0].Key != "b" || g[0].Vendors[1].Key != "a" || g[0].Expenses["b"] != 2 {
		t.Fatalf("groups: %+v", g)
	}
}

// A vendor archived by a merge is no duplicate any more: listed again
// with 0 expenses, the merge never ended.
func TestVendorDuplicatesSkipArchived(t *testing.T) {
	n := &sources.NinjaDataset{
		Vendors:  []sources.NinjaVendor{{Key: "a", Name: "Tonerwelt GmbH", Archived: true}, {Key: "b", Name: "Tonerwelt"}},
		Expenses: []sources.NinjaExpense{{VendorKey: "b"}, {VendorKey: "b"}},
	}
	if g := metrics.VendorDuplicates(n); len(g) != 0 {
		t.Fatalf("groups: %+v", g)
	}
}

// A mixed account: its incomes count like the business account's, a
// debit to a vendor with expenses is proposed, the rest waits apart.
func TestBankCasesMixed(t *testing.T) {
	ninja, sure := vatBook()
	for i := range ninja.BankTxns {
		if ninja.BankTxns[i].Role == sources.RolePrivate {
			ninja.BankTxns[i].Role = sources.RoleMixed
		}
	}
	in := september()
	in.Ninja, in.Sure = ninja, sure
	got := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(in) {
		got[c.Txn.Key] = c
	}
	if c, ok := got["b8"]; !ok || !c.Waiting() || c.Private || !c.Mixed {
		t.Errorf("income on a mixed account: %+v", c)
	}
	if c := got["b6"]; c.Action != metrics.ActionCreate || !c.Waiting() || !c.Private || !c.Mixed {
		t.Errorf("known vendor: %+v", c)
	}
	if c := got["b7"]; c.Action != metrics.ActionNone || c.Waiting() {
		t.Errorf("unknown vendor waits apart: %+v", c)
	}
}

// A card (transfer account): its purchases are bookings; the monthly
// settlement from the giro and its income on the card are a transfer.
func TestBankCasesCard(t *testing.T) {
	n := &sources.NinjaDataset{
		BankTxns: []sources.NinjaBankTxn{
			{Key: "buy", Date: "2026-09-03", Payer: "Bauhaus", Amount: 120, Debit: true, Role: sources.RoleTransfer, Account: "Karte"},
			{Key: "settle", Date: "2026-09-28", Payer: "Kartenabrechnung", Amount: 120, Debit: true, Role: sources.RoleBusiness, Account: "Giro"},
			{Key: "paid", Date: "2026-09-29", Payer: "Zahlung", Amount: 120, Role: sources.RoleTransfer, Account: "Karte"},
		},
	}
	start, _ := metrics.ParseDay("2026-09-01")
	end, _ := metrics.ParseDay("2026-09-30")
	got := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(metrics.VATInputs{Ninja: n, Start: start, End: end}) {
		got[c.Txn.Key] = c
	}
	if c := got["buy"]; c.Action != metrics.ActionCreate || c.Transfer != nil {
		t.Errorf("purchase: %+v", c)
	}
	if c := got["settle"]; c.Action != metrics.ActionSkip || c.Transfer == nil || c.Transfer.Account != "Karte" {
		t.Errorf("settlement: %+v", c)
	}
	if c := got["paid"]; c.Action != metrics.ActionSkip || c.Transfer == nil || c.Transfer.Account != "Giro" {
		t.Errorf("card income: %+v", c)
	}
}

// A booking that fits an invoice, payment or expense is no transfer's
// other side: a card purchase stays an expense though a customer pays
// the same amount on the giro two days later, and a refund on the card
// does not take a giro debit an expense explains.
func TestBankCasesTransferNotBooked(t *testing.T) {
	n := &sources.NinjaDataset{
		Clients:  []sources.NinjaClient{{ID: 1, Name: "Northlight Pictures"}},
		Invoices: []sources.NinjaInvoice{{ID: 7, Number: "R-17", ClientID: 1, Status: "sent", Amount: 119, Balance: 119}},
		Expenses: []sources.NinjaExpense{{Key: "e1", Date: "2026-09-11", Amount: 50}},
		BankTxns: []sources.NinjaBankTxn{
			{Key: "buy", Date: "2026-09-03", Payer: "Bauhaus", Amount: 119, Debit: true, Role: sources.RoleTransfer, Account: "Karte"},
			{Key: "income", Date: "2026-09-05", Payer: "Northlight Pictures", Amount: 119, Role: sources.RoleBusiness, Account: "Giro"},
			{Key: "refund", Date: "2026-09-10", Payer: "Bauhaus Erstattung", Amount: 50, Role: sources.RoleTransfer, Account: "Karte"},
			{Key: "spent", Date: "2026-09-11", Payer: "Studiobedarf", Amount: 50, Debit: true, Role: sources.RoleBusiness, Account: "Giro"},
		},
	}
	start, _ := metrics.ParseDay("2026-09-01")
	end, _ := metrics.ParseDay("2026-09-30")
	got := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(metrics.VATInputs{Ninja: n, Start: start, End: end}) {
		got[c.Txn.Key] = c
	}
	if c := got["buy"]; c.Action != metrics.ActionCreate || c.Transfer != nil {
		t.Errorf("purchase: %+v", c.Transfer)
	}
	if c := got["income"]; c.Action != metrics.ActionInvoice {
		t.Errorf("income: %+v", c)
	}
	if c := got["refund"]; c.Transfer != nil {
		t.Errorf("refund bound to an expense's debit: %+v", c.Transfer)
	}
	if c := got["spent"]; c.Action != metrics.ActionLink {
		t.Errorf("giro debit: %+v", c)
	}
}

// Once the customer's payment is booked, its Sure twin is no other side
// either; nor is a private income of the same amount: a card purchase
// has a transfer only as a payout to a business or mixed account.
func TestBankCasesTransferBookedTwin(t *testing.T) {
	n := &sources.NinjaDataset{
		BankTxns: []sources.NinjaBankTxn{
			{Key: "buy", Date: "2026-09-03", Payer: "Bauhaus", Amount: 119, Debit: true, Role: sources.RoleTransfer, Account: "Karte"},
			{Key: "income", Date: "2026-09-05", Payer: "Northlight Pictures", Amount: 119, Role: sources.RoleBusiness, Account: "Giro", Status: sources.BankDone, PaymentKey: "p3"},
			{Key: "buy2", Date: "2026-09-20", Payer: "Obi", Amount: 64, Debit: true, Role: sources.RoleTransfer, Account: "Karte"},
			{Key: "friend", Date: "2026-09-21", Payer: "Jonas", Amount: 64, Role: sources.RolePrivate, Account: "Privat"},
		},
	}
	sure := &sources.BankData{Transactions: []sources.BankTxn{
		{ID: "s1", Date: "2026-09-05", Name: "Northlight Pictures", Amount: 119, Account: "Giro", Role: sources.RoleBusiness},
	}}
	start, _ := metrics.ParseDay("2026-09-01")
	end, _ := metrics.ParseDay("2026-09-30")
	got := map[string]metrics.BankCase{}
	for _, c := range metrics.BankCases(metrics.VATInputs{Ninja: n, Sure: sure, Start: start, End: end}) {
		got[c.Txn.Key] = c
	}
	for _, k := range []string{"buy", "buy2"} {
		if c := got[k]; c.Action != metrics.ActionCreate || c.Transfer != nil {
			t.Errorf("%s: %s %+v", k, c.Action, c.Transfer)
		}
	}
}

// A transfer needs both accounts marked, and holds both ways: a PayPal
// payout to a giro not yet marked is no transfer from either side (it
// was once a transfer for the giro and an expense for PayPal); marked
// as business, both sides pair.
func TestBankCasesTransferMarked(t *testing.T) {
	cases := func(giro sources.AccountRole) map[string]metrics.BankCase {
		n := &sources.NinjaDataset{BankTxns: []sources.NinjaBankTxn{
			{Key: "payout", Date: "2026-09-08", Payer: "PayPal Auszahlung", Amount: 200, Debit: true, Role: sources.RoleTransfer, Account: "PayPal"},
			{Key: "in", Date: "2026-09-10", Payer: "PayPal", Amount: 200, Role: giro, Account: "Giro"},
		}}
		start, _ := metrics.ParseDay("2026-09-01")
		end, _ := metrics.ParseDay("2026-09-30")
		got := map[string]metrics.BankCase{}
		for _, c := range metrics.BankCases(metrics.VATInputs{Ninja: n, Start: start, End: end}) {
			got[c.Txn.Key] = c
		}
		return got
	}
	got := cases(sources.RoleUnknown)
	if got["payout"].Transfer != nil || got["in"].Transfer != nil {
		t.Errorf("unmarked giro: payout %+v, in %+v", got["payout"].Transfer, got["in"].Transfer)
	}
	got = cases(sources.RoleBusiness)
	if got["payout"].Transfer == nil || got["payout"].Transfer.Account != "Giro" || got["in"].Transfer == nil {
		t.Errorf("business giro: payout %+v, in %+v", got["payout"].Transfer, got["in"].Transfer)
	}
}

// Receipts: a card purchase needs one, a booking between own accounts
// never; on a mixed account only spending at a vendor with expenses.
func TestMissingReceiptsCardMixed(t *testing.T) {
	ninja, _ := vatBook()
	sure := &sources.BankData{Accounts: []sources.BankAccount{{Name: "Karte", Role: sources.RoleTransfer}, {Name: "Mix", Role: sources.RoleMixed}},
		Transactions: []sources.BankTxn{
			{ID: "c1", Date: "2026-09-03", Name: "Bauhaus", Amount: -120, Account: "Karte", Role: sources.RoleTransfer},
			{ID: "c2", Date: "2026-09-28", Name: "Kartenabrechnung", Amount: -300, Account: "Mix", Role: sources.RoleMixed, Transfer: true},
			{ID: "m1", Date: "2026-09-05", Name: "Elbnetz Mobilfunk", Amount: -39.95, Account: "Mix", Role: sources.RoleMixed},
			{ID: "m2", Date: "2026-09-06", Name: "Supermarkt", Amount: -64.2, Account: "Mix", Role: sources.RoleMixed},
		}}
	ninja.Expenses = ninja.Expenses[:1] // Elbnetz's receipts must not cover m1
	ninja.Expenses[0].VendorKey = "v2"
	in := metrics.ReceiptInputs{Sure: sure, Ninja: ninja, MinAmount: 5, Window: 10, Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	var names []string
	for _, tx := range metrics.MissingReceipts(in) {
		names = append(names, tx.Name)
	}
	if !slices.Equal(names, []string{"Bauhaus", "Elbnetz Mobilfunk"}) {
		t.Fatalf("missing: %v", names)
	}
}
