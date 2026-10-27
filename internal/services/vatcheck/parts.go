package vatcheck

import (
	"slices"

	"andon/internal/metrics"
)

// Part is one of the ten things a finished expense has (owner,
// 27.10.2026). The booking brings amount, currency and date; the rest
// come from the draft, the scan and the user:
//
//	vendor  category  tax  amount  currency  date  euer  number  scan  note
//	  ■        ■       ■     ■        ■       ■     □      ■      ■     ■   9 / 10
type Part string

const (
	PartVendor   Part = "vendor"
	PartCategory Part = "category"
	PartTax      Part = "tax"
	PartAmount   Part = "amount"
	PartCurrency Part = "currency"
	PartDate     Part = "date"
	PartEuer     Part = "euer"
	PartNumber   Part = "number"
	PartScan     Part = "scan"
	PartNote     Part = "note"
)

// Parts lists them in the order the page shows them.
var Parts = []Part{PartVendor, PartCategory, PartTax, PartAmount, PartCurrency, PartDate, PartEuer, PartNumber, PartScan, PartNote}

// partsOf leaves out the parts Invoice Ninja has no field for: the EÜR
// line and the invoice number are custom fields the user picks.
func partsOf(euer, number bool) []Part {
	return slices.DeleteFunc(slices.Clone(Parts), func(p Part) bool {
		return (p == PartEuer && !euer) || (p == PartNumber && !number)
	})
}

// Creates says whether the page starts with a new expense: only then it
// counts its parts.
func (c Case) Creates() bool { return c.Txn.Debit && c.Act() == metrics.ActionCreate }

// Has says whether the proposed expense has part p already.
func (c Case) Has(p Part) bool {
	switch p {
	case PartVendor:
		return c.Draft.Vendor != ""
	case PartCategory:
		return c.Draft.CategoryKey != ""
	case PartTax:
		return c.Draft.TaxKnown
	case PartEuer:
		return c.Draft.Euer != ""
	case PartNumber:
		return c.Number != ""
	case PartScan:
		return c.Scan() != 0
	case PartNote:
		return c.Note != ""
	}
	return true // amount, currency and date: the booking's
}

// Done counts the parts it has.
func (c Case) Done() int {
	n := 0
	for _, p := range c.parts {
		if c.Has(p) {
			n++
		}
	}
	return n
}

// Ready says whether the case can be taken as it is: a new expense with
// all its parts, or a link, invoice or payment found.
func (c Case) Ready() bool {
	switch c.Act() {
	case metrics.ActionCreate:
		return c.Creates() && c.Done() == len(c.parts)
	case metrics.ActionLink, metrics.ActionInvoice, metrics.ActionPayment:
		return true
	}
	return false
}

// Scan is the scan proposed for a new expense: the best candidate when
// it scores high, else 0.
func (c Case) Scan() int64 {
	if len(c.Scans) == 0 || c.Scans[0].Level() != scanPicked {
		return 0
	}
	return c.Scans[0].Doc.ID
}

// scanPicked is the level from which the best scan is proposed.
const scanPicked = "high"

// Head counts the open work of the period, for the list's head:
//
//	12 open · 3 without a scan · 2 without an EÜR line · 7 / 12 ready
type Head struct {
	Open, NoScan, NoEuer, Ready int
}

// Head counts the cases.
func (p Page) Head() Head {
	h := Head{Open: len(p.Cases)}
	for _, c := range p.Cases {
		if c.Ready() {
			h.Ready++
		}
		if !c.Creates() {
			continue
		}
		if !c.Has(PartScan) {
			h.NoScan++
		}
		if slices.Contains(c.parts, PartEuer) && !c.Has(PartEuer) {
			h.NoEuer++
		}
	}
	return h
}

// Parts lists the parts the case counts, for the page.
func (c Case) Parts() []Part { return c.parts }

// Acts lists what the case can do: link an expense found, create one
// (debits), book an invoice or a payment found, or archive it.
func (c Case) Acts() []metrics.BankAction {
	var out []metrics.BankAction
	if len(c.Expenses) > 0 {
		out = append(out, metrics.ActionLink)
	}
	if c.Txn.Debit {
		out = append(out, metrics.ActionCreate)
	}
	if c.Invoice != nil {
		out = append(out, metrics.ActionInvoice)
	}
	if c.Payment != nil {
		out = append(out, metrics.ActionPayment)
	}
	return append(out, metrics.ActionSkip)
}

// Act is the action the page starts with: the proposal, else the first
// it can do.
func (c Case) Act() metrics.BankAction {
	acts := c.Acts()
	if slices.Contains(acts, c.Action) {
		return c.Action
	}
	return acts[0]
}

// Picked says whether the page takes the case without asking: a link,
// invoice or payment found, or a new expense like the vendor's last.
func (c Case) Picked() bool {
	if !c.Waiting() {
		return false
	}
	switch c.Action {
	case metrics.ActionLink, metrics.ActionInvoice, metrics.ActionPayment:
		return true
	case metrics.ActionCreate:
		return c.Draft.TaxKnown && c.Draft.From == metrics.FromHistory
	}
	return false
}
