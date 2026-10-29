package vatcheck

import (
	"slices"
	"strings"

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
// line and the invoice number are custom fields the user picks, the scan
// needs Paperless paired (receipts).
func partsOf(euer, number, scan bool) []Part {
	return slices.DeleteFunc(slices.Clone(Parts), func(p Part) bool {
		return (p == PartEuer && !euer) || (p == PartNumber && !number) || (p == PartScan && !scan)
	})
}

// ActionFill fills what a linked expense of the period lacks (the gaps
// list): nothing is booked, the expense is completed.
const ActionFill metrics.BankAction = "fill"

// ActionReceipt creates an expense from a scan no booking pays (cash or
// private money): nothing is linked in the bank feed.
const ActionReceipt metrics.BankAction = "receipt"

// fillPrefix starts a gap case's key: "x-" + the expense's key;
// docPrefix a receipt case's: "d-" + the scan's id.
const (
	fillPrefix = "x-"
	docPrefix  = "d-"
)

// Counts says whether the page counts the case's parts: a new expense,
// one found to link, or one to fill.
func (c Case) Counts() bool {
	switch c.Act() {
	case metrics.ActionCreate, metrics.ActionLink, ActionFill, ActionReceipt:
		return c.Txn.Debit
	}
	return false
}

// Has says whether the expense has part p already. One found or filled
// has what it does not lack; a gap counts once a value is proposed.
func (c Case) Has(p Part) bool {
	if act := c.Act(); (act == metrics.ActionLink || act == ActionFill) && !slices.Contains(c.gaps, p) {
		return true
	}
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
	case PartAmount:
		return c.Act() != ActionReceipt || c.Txn.Amount > 0 // a scan may have none
	}
	return true // currency and date: the booking's
}

// Gap says whether the expense found or filled lacks part p, for the
// desk's fields.
func (c Case) Gap(p Part) bool { return slices.Contains(c.gaps, p) }

// Lacks says whether the expense found or filled lacks a part.
func (c Case) Lacks() bool { return len(c.gaps) > 0 }

// Offers says whether any expense the case may take lacks part p: the
// page has its field, shown while that expense is picked.
func (c Case) Offers(p Part) bool {
	for _, gaps := range c.lacks {
		if slices.Contains(gaps, p) {
			return true
		}
	}
	return false
}

// Offering says whether any expense the case may take lacks a part.
func (c Case) Offering() bool {
	for _, gaps := range c.lacks {
		if len(gaps) > 0 {
			return true
		}
	}
	return false
}

// GapsOf are expense key's gaps for the page: "euer scan".
func (c Case) GapsOf(key string) string {
	var out []string
	for _, p := range c.lacks[key] {
		out = append(out, string(p))
	}
	return strings.Join(out, " ")
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

// Ready says whether the case can be taken as it is: an expense with
// all its parts, or an invoice or payment found.
func (c Case) Ready() bool {
	switch c.Act() {
	case metrics.ActionCreate, metrics.ActionLink, ActionFill, ActionReceipt:
		return c.Counts() && c.Done() == len(c.parts)
	case metrics.ActionInvoice, metrics.ActionPayment:
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
		if !c.Counts() {
			continue
		}
		if slices.Contains(c.parts, PartScan) && !c.Has(PartScan) {
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
// (debits), book an invoice or a payment found, or archive it; a gap
// case only fills.
func (c Case) Acts() []metrics.BankAction {
	if c.Action == ActionFill || c.Action == ActionReceipt {
		return []metrics.BankAction{c.Action}
	}
	var out []metrics.BankAction
	if len(c.Expenses) > 0 {
		out = append(out, metrics.ActionLink)
	}
	if c.Txn.Debit {
		out = append(out, metrics.ActionCreate, ActionSplit)
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
// invoice or payment found, a new expense like the vendor's last, or
// gaps all proposed.
func (c Case) Picked() bool {
	if !c.Waiting() {
		return false
	}
	switch c.Action {
	case metrics.ActionLink, metrics.ActionInvoice, metrics.ActionPayment:
		return true
	case metrics.ActionCreate:
		return c.Draft.TaxKnown && c.Draft.From == metrics.FromHistory
	case ActionFill:
		return c.Ready()
	}
	return false // a receipt: cash or private is the user's word
}
