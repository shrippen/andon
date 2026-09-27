package sources

import (
	"strconv"
	"strings"
	"time"
)

// Demo receipts: the demo Invoice Ninja's expenses and the demo
// Paperless' scans, set up so each view of the receipts page shows
// something:
//
//	Lenovo    invoice number in both → sure match
//	Hetzner   linked already
//	Adobe     amount and vendor only
//	Telekom   the demo Paperless' invoice (311)
//	Schmidt   two receipts that add up (1∶n)
//	Bahn      no scan yet
//	Aral      a scan tagged "Beleg" without expense (receipts first)

const (
	demoDocsURL = "https://docs.demo"
	// DemoReceiptTag is the demo Paperless' tag of scans waiting for an expense.
	DemoReceiptTag = "Beleg"
)

// Demo Paperless custom field ids.
const (
	demoFieldInvoice int64 = iota + 1
	demoFieldExpense
	demoFieldLink
	demoFieldAmount
)

type demoReceipt struct {
	expense, number, vendor, notes, invoice string
	amount                                  float64
	back                                    int // days before today
	docs                                    []demoScan
	linked                                  bool
}

type demoScan struct {
	id                         int64
	title, correspondent, text string
	amount                     float64
	back                       int
	tagged                     bool
}

var demoReceipts = []demoReceipt{
	{expense: "demo1", number: "EX-0041", vendor: "Lenovo", notes: "Notebook ThinkPad", invoice: "LEN-88213", amount: 1428, back: 30,
		docs: []demoScan{{id: 201, title: "Rechnung LEN-88213", correspondent: "Lenovo (Deutschland) GmbH", text: "Rechnungsnr. LEN-88213 Gesamtbetrag 1.428,00 EUR", amount: 1428, back: 29}}},
	{expense: "demo2", number: "EX-0042", vendor: "Hetzner Online", notes: "Hosting", invoice: "R0012345678", amount: 59.5, back: 12, linked: true,
		docs: []demoScan{{id: 202, title: "Rechnung Hetzner", correspondent: "Hetzner Online GmbH", text: "Rechnung R0012345678 Summe 59,50 EUR", amount: 59.5, back: 12}}},
	{expense: "demo3", number: "EX-0043", vendor: "Adobe", notes: "Software", amount: 238, back: 5,
		docs: []demoScan{{id: 203, title: "Adobe Creative Cloud", correspondent: "Adobe Systems Software Ireland", text: "Total 238,00 EUR", back: 4}}},
	{expense: "demo4", number: "EX-0044", vendor: "Telekom Deutschland GmbH", notes: "Mobilfunk", amount: 39.95, back: 23,
		docs: []demoScan{{id: 311, title: "Rechnung 09/2026", correspondent: "Telekom Deutschland GmbH", text: "Rechnungsbetrag 39,95 EUR", amount: 39.95, back: 23}}},
	{expense: "demo5", number: "EX-0045", vendor: "Bürobedarf Schmidt", notes: "Büromaterial", amount: 86.4, back: 9,
		docs: []demoScan{{id: 204, title: "Quittung Schmidt", correspondent: "Bürobedarf Schmidt", text: "Summe 50,40", amount: 50.4, back: 9, tagged: true},
			{id: 205, title: "Quittung Schmidt", correspondent: "Bürobedarf Schmidt", text: "Summe 36,00", amount: 36, back: 8, tagged: true}}},
	{expense: "demo6", number: "EX-0046", vendor: "Deutsche Bahn", notes: "Reise Kundentermin", amount: 129.9, back: 16},
	{docs: []demoScan{{id: 206, title: "Tankquittung", correspondent: "Aral", text: "Betrag 72,18 EUR", amount: 72.18, back: 3, tagged: true}}},
}

// DemoExpenses is the demo Invoice Ninja's expense list.
func DemoExpenses(now time.Time) *ExpenseSet {
	today := demoDay(now)
	set := &ExpenseSet{URL: "https://invoices.demo", Slots: [NinjaSlots]string{"Rechnungsnummer", "Paperless"}}
	for _, r := range demoReceipts {
		if r.expense == "" {
			continue
		}
		e := ReceiptExpense{Key: r.expense, Number: r.number, Vendor: r.vendor, Notes: r.notes, Amount: r.amount,
			Day: iso(today.AddDate(0, 0, -r.back)), Updated: today.AddDate(0, 0, -r.back)}
		e.Custom[0] = r.invoice
		if r.linked {
			e.Custom[1] = demoDocURL(r.docs[0].id)
		}
		set.Expenses = append(set.Expenses, e)
	}
	return set
}

// DemoDocs is the demo Paperless' documents of one year (0 = all).
func DemoDocs(now time.Time, year int) *DocSet {
	today := demoDay(now)
	set := &DocSet{URL: demoDocsURL, Tags: map[string]int64{strings.ToLower(DemoReceiptTag): 1},
		Fields: []DocField{{demoFieldInvoice, "Rechnungsnummer", "string"}, {demoFieldExpense, "Ausgabe", "string"},
			{demoFieldLink, "Invoice Ninja", "url"}, {demoFieldAmount, "Betrag", "monetary"}}}
	for _, r := range demoReceipts {
		for _, s := range r.docs {
			created := today.AddDate(0, 0, -s.back)
			if year != 0 && created.Year() != year {
				continue
			}
			doc := ReceiptDoc{ID: s.id, Title: s.title, Correspondent: s.correspondent, Day: iso(created), Added: iso(created),
				Content: s.text, Custom: map[int64]any{}}
			if s.amount > 0 {
				doc.Custom[demoFieldAmount] = "EUR" + strconv.FormatFloat(s.amount, 'f', 2, 64)
			}
			if r.linked {
				doc.Custom[demoFieldExpense] = r.number
				doc.Custom[demoFieldInvoice] = r.invoice
				doc.Custom[demoFieldLink] = "https://invoices.demo/expenses/" + r.expense + "/edit"
			}
			if s.tagged {
				doc.Tags = []int64{1}
			}
			set.Docs = append(set.Docs, doc)
		}
	}
	return set
}

// DemoReceiptOptions are the demo connections' field mappings, as the
// receipts page stores them.
func DemoReceiptOptions() (ninja, paperless map[string]any) {
	return map[string]any{"receipt_invoice_slot": 1, "receipt_link_slot": 2},
		map[string]any{"receipt_field_invoice": demoFieldInvoice, "receipt_field_expense": demoFieldExpense,
			"receipt_field_link": demoFieldLink, "receipt_field_amount": demoFieldAmount, "receipt_queue_tag": DemoReceiptTag}
}

func demoDocURL(id int64) string {
	return demoDocsURL + "/documents/" + strconv.FormatInt(id, 10) + "/"
}

// demoSearch filters the demo documents like Paperless would, roughly.
func demoSearch(now time.Time, s DocSearch) []ReceiptDoc {
	var out []ReceiptDoc
	for _, doc := range DemoDocs(now, 0).Docs {
		text := strings.ToLower(doc.Title + " " + doc.Correspondent + " " + doc.Content)
		for _, want := range []string{s.Query, s.TitleContent, s.Correspondent} {
			if want != "" && !strings.Contains(text, strings.ToLower(want)) {
				text = ""
			}
		}
		if text == "" || (s.From != "" && doc.Day < s.From) || (s.To != "" && doc.Day > s.To) {
			continue
		}
		out = append(out, doc)
	}
	return out
}
