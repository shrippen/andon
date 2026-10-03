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
// Paperless' scans, from the world's receipts and the cases in
// "documents.receipts", set up so each view of the receipts page shows
// something: a sure match, a link, amount and vendor only, the Paperless
// invoice, two scans that add up (1∶n), no scan yet, a scan without expense.

// demoReceiptSetup is the world's "documents.receipts".
type demoReceiptSetup struct {
	URL, Tag string
	Slots    []string
	Fields   [4]string
	Cases    []struct {
		Receipt         int
		Vendor          string
		Day             int
		Expense, Number string
		Invoice, Linked bool
		Scans           []struct {
			ID                         int64
			Title, Correspondent, Text string
			Amount, Rest, Tagged       bool
			Part                       float64
			Day                        int
		}
	}
}

func receiptsOf(now time.Time) *demoReceiptSetup {
	r := &demoReceiptSetup{}
	demoworld.MustDecode("documents.receipts", now, r)
	return r
}

// demoDocsURL is the demo Paperless.
func demoDocsURL() string {
	var docs struct{ URL string }
	demoworld.MustDecode("documents", time.Now(), &docs)
	return docs.URL
}

// DemoReceiptTag is the demo Paperless' tag of scans waiting for an expense.
func DemoReceiptTag() string { return receiptsOf(time.Now()).Tag }

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
	euro := func(v float64) string { return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1) }
	var out []demoReceipt
	for _, c := range receiptsOf(today).Cases {
		r := demoReceipt{expense: c.Expense, number: c.Number, linked: c.Linked}
		if c.Vendor != "" {
			v := demoWorld.Vendor(c.Vendor)
			r.vendor, r.notes, r.amount, r.day = v.Name, v.Kind.DE(), v.Monthly, today.AddDate(0, 0, c.Day)
		} else {
			w := demoWorld.Receipt(c.Receipt)
			r.vendor, r.notes, r.amount, r.day = w.Vendor, w.Note.DE(), w.Amount, monday.AddDate(0, 0, w.Day)
			if c.Invoice {
				r.invoice = w.Number
			}
		}

		// A scan carries the whole amount, a part of it or the rest.
		rest := r.amount
		for _, s := range c.Scans {
			amount := r.amount
			switch {
			case s.Part > 0:
				amount = s.Part
				rest -= s.Part
			case s.Rest:
				amount = round2(rest)
			}
			scan := demoScan{id: s.ID, title: s.Title, correspondent: s.Correspondent, day: r.day.AddDate(0, 0, s.Day),
				text: strings.ReplaceAll(s.Text, "{amount}", euro(amount)), tagged: s.Tagged}
			if s.Amount || s.Part > 0 || s.Rest {
				scan.amount = amount
			}
			r.docs = append(r.docs, scan)
		}
		out = append(out, r)
	}
	return out
}

// DemoExpenses is the demo Invoice Ninja's expense list.
func DemoExpenses(now time.Time) *ExpenseSet {
	today := demoDay(now)
	setup := receiptsOf(today)
	set := &ExpenseSet{URL: setup.URL}
	copy(set.Slots[:], setup.Slots)
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
	setup := receiptsOf(today)
	f := setup.Fields
	set := &DocSet{URL: demoDocsURL(), Tags: map[string]int64{strings.ToLower(setup.Tag): 1}, TagNames: []string{setup.Tag},
		Fields: []DocField{{demoFieldInvoice, f[0], "string"}, {demoFieldExpense, f[1], "string"},
			{demoFieldLink, f[2], "url"}, {demoFieldAmount, f[3], "monetary"}}}
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
				doc.Custom[demoFieldLink] = setup.URL + "/expenses/" + r.expense + "/edit"
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
			"receipt_field_link": demoFieldLink, "receipt_field_amount": demoFieldAmount, "receipt_queue_tag": DemoReceiptTag()}
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
	return demoDocsURL() + "/documents/" + strconv.FormatInt(id, 10) + "/"
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
