package sources

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources/demoworld"
)

// Demo receipts: the demo Invoice Ninja's expenses and the demo
// Paperless' scans, taken from the receipts of Studio Weber (package
// demoworld, the same ones as in DemoInvoiceNinja), set up so each view of
// the receipts page shows something:
//
//	Fotohaus Elbe           invoice number in both → sure match
//	Kabelwerk Studiobedarf  linked already
//	Kombüse Catering        amount and vendor only
//	Elbnetz Mobilfunk       the demo Paperless' invoice (311)
//	Mietwagen Nord          two receipts that add up (1∶n)
//	Druckerei Nordlicht     no scan yet
//	Tankstelle Elbchaussee  a scan tagged "Beleg" without expense (receipts first)

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
	day                                     time.Time
	docs                                    []demoScan
	linked                                  bool
}

type demoScan struct {
	id                         int64
	title, correspondent, text string
	amount                     float64
	day                        time.Time
	tagged                     bool
}

// demoReceipts builds the list for today; dates of the demo world's
// receipts are days from Monday of this week.
func demoReceipts(today time.Time) []demoReceipt {
	monday := demoMonday(today)
	w := func(id int) (demoworld.Receipt, time.Time) {
		r := demoWorld.Receipt(id)
		return r, monday.AddDate(0, 0, r.Day)
	}
	euro := func(v float64) string { return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1) }

	film, filmDay := w(1)
	cable, cableDay := w(2)
	catering, cateringDay := w(3)
	car, carDay := w(4)
	fuel, fuelDay := w(5)
	printing, printingDay := w(6)
	phoneDay := today.AddDate(0, 0, -23) // DemoPaperless' invoice 311
	phone := demoMobile
	return []demoReceipt{
		{expense: "demo1", number: "EX-0041", vendor: film.Vendor, notes: film.Note.DE(), invoice: film.Number, amount: film.Amount, day: filmDay,
			docs: []demoScan{{id: 201, title: "Rechnung " + film.Number, correspondent: film.Vendor + " GmbH",
				text: "Rechnungsnr. " + film.Number + " Gesamtbetrag " + euro(film.Amount) + " EUR", amount: film.Amount, day: filmDay.AddDate(0, 0, 1)}}},
		{expense: "demo2", number: "EX-0042", vendor: cable.Vendor, notes: cable.Note.DE(), invoice: cable.Number, amount: cable.Amount, day: cableDay, linked: true,
			docs: []demoScan{{id: 202, title: "Rechnung " + cable.Vendor, correspondent: cable.Vendor,
				text: "Rechnung " + cable.Number + " Summe " + euro(cable.Amount) + " EUR", amount: cable.Amount, day: cableDay}}},
		{expense: "demo3", number: "EX-0043", vendor: catering.Vendor, notes: catering.Note.DE(), amount: catering.Amount, day: cateringDay,
			docs: []demoScan{{id: 203, title: catering.Vendor, correspondent: catering.Vendor, text: "Total " + euro(catering.Amount) + " EUR",
				day: cateringDay.AddDate(0, 0, 1)}}},
		{expense: "demo4", number: "EX-0044", vendor: phone.Name, notes: phone.Kind.DE(), amount: phone.Monthly, day: phoneDay,
			docs: []demoScan{{id: 311, title: "Rechnung 09/2026", correspondent: phone.Name, text: "Rechnungsbetrag " + euro(phone.Monthly) + " EUR", amount: phone.Monthly, day: phoneDay}}},
		{expense: "demo5", number: "EX-0045", vendor: car.Vendor, notes: car.Note.DE(), amount: car.Amount, day: carDay,
			docs: []demoScan{{id: 204, title: "Quittung " + car.Vendor, correspondent: car.Vendor, text: "Summe 99,00", amount: 99, day: carDay, tagged: true},
				{id: 205, title: "Quittung " + car.Vendor, correspondent: car.Vendor, text: "Summe " + euro(car.Amount-99), amount: round2(car.Amount - 99),
					day: carDay.AddDate(0, 0, 1), tagged: true}}},
		{expense: "demo6", number: "EX-0046", vendor: printing.Vendor, notes: printing.Note.DE(), amount: printing.Amount, day: printingDay},
		{docs: []demoScan{{id: 206, title: "Tankquittung", correspondent: fuel.Vendor, text: "Betrag " + euro(fuel.Amount) + " EUR",
			amount: fuel.Amount, day: fuelDay, tagged: true}}},
	}
}

// DemoExpenses is the demo Invoice Ninja's expense list.
func DemoExpenses(now time.Time) *ExpenseSet {
	today := demoDay(now)
	set := &ExpenseSet{URL: "https://invoices.demo", Slots: [NinjaSlots]string{"Rechnungsnummer", "Paperless"}}
	for _, r := range demoReceipts(today) {
		if r.expense == "" {
			continue
		}
		e := ReceiptExpense{Key: r.expense, Number: r.number, Vendor: r.vendor, Notes: r.notes, Amount: r.amount,
			Day: iso(r.day), Updated: r.day}
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
	set := &DocSet{URL: demoDocsURL, Tags: map[string]int64{strings.ToLower(DemoReceiptTag): 1}, TagNames: []string{DemoReceiptTag},
		Fields: []DocField{{demoFieldInvoice, "Rechnungsnummer", "string"}, {demoFieldExpense, "Ausgabe", "string"},
			{demoFieldLink, "Invoice Ninja", "url"}, {demoFieldAmount, "Betrag", "monetary"}}}
	for _, r := range demoReceipts(today) {
		for _, s := range r.docs {
			created := s.day
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

// demoThumb draws a demo scan as a small paper receipt (SVG): who,
// what, when and how much, the way a thumbnail shows a real one.
func demoThumb(now time.Time, id int64) ([]byte, string, error) {
	for _, doc := range DemoDocs(now, 0).Docs {
		if doc.ID != id {
			continue
		}
		amount, _ := doc.Custom[demoFieldAmount].(string)
		if amount = strings.Replace(strings.TrimPrefix(amount, "EUR"), ".", ",", 1); amount != "" {
			amount += " €"
		}
		day := doc.Day
		if t, err := time.Parse(time.DateOnly, doc.Day); err == nil {
			day = t.Format("02.01.2006")
		}
		lines := ""
		for i := range 4 {
			lines += fmt.Sprintf(`<rect x="12" y="%d" width="%d" height="3" fill="#c9c3b3"/>`, 78+i*9, 76-i%2*18)
		}
		svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 140" width="100" height="140">`+
			`<rect width="100" height="140" fill="#fdfcf8"/><rect x=".5" y=".5" width="99" height="139" fill="none" stroke="#d8d2c4"/>`+
			`<text x="12" y="24" font-family="sans-serif" font-size="9" font-weight="700" fill="#2b2b2b">%s</text>`+
			`<text x="12" y="38" font-family="sans-serif" font-size="7" fill="#555">%s</text>`+
			`<text x="12" y="50" font-family="sans-serif" font-size="7" fill="#555">%s</text>%s`+
			`<rect x="12" y="116" width="76" height=".8" fill="#2b2b2b"/>`+
			`<text x="88" y="130" font-family="sans-serif" font-size="10" font-weight="700" fill="#2b2b2b" text-anchor="end">%s</text></svg>`,
			html.EscapeString(shortText(doc.Correspondent, 16)), html.EscapeString(shortText(doc.Title, 22)), day, lines, html.EscapeString(amount))
		return []byte(svg), "image/svg+xml", nil
	}
	return nil, "", newSourceError("demo: no document %d", id)
}

func shortText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
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
