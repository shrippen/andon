package vatcheck

// Split: one debit pays several invoices (an Amazon order of three
// sellers). Each invoice becomes an expense of its own, with its vendor,
// number and tax; all hang on the same booking:
//
//	Amazon Payments −84,37
//	  ├─ Amazon EU      39,99  19 %  scan DE5-…   new expense
//	  ├─ Kabelprofi24   14,49  19 %  EX-0063      expense found, linked
//	  └─ Buchhaus       29,89   7 %  scan RE-…    new expense, new vendor
//	                    ─────
//	                    84,37 = the booking: it can be taken
//
// The desk offers the free scans and unlinked expenses near the booking
// that are smaller than it; a scan naming the booking's order number is
// ticked. The form sends each part as slot fields "s<i>.<field>".

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/receipts"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// ActionSplit books a debit against several expenses, one per invoice.
const ActionSplit metrics.BankAction = "split"

const (
	// maxParts is how many parts a split has at most: slots on the desk.
	maxParts = 8
	// splitDays is how far a part's invoice may lie from the booking.
	splitDays = 10
)

// FieldUse says what a slot is: an expense (new or found) or a private
// part, which counts to the sum but becomes no expense; "" leaves it out.
const (
	FieldUse   = "use"
	UseExpense = "expense"
	UsePrivate = "private"
)

const (
	ProblemSplitSum   workbench.Problem = "vatcheck.problem_split_sum"   // the parts are not the booking's amount
	ProblemSplitParts workbench.Problem = "vatcheck.problem_split_parts" // fewer than two parts, or none an expense
)

// partNames are a slot's fields.
var partNames = []string{FieldUse, FieldScan, FieldExpense, FieldVendor, FieldCategory, FieldTax, FieldEuer, FieldNumber, FieldNote, FieldAmount, FieldDay}

// The split's switches hold for every new part; own names, as a new
// expense's (gross, paid, bill) sit in the same desk.
const (
	FieldSplitGross = "sgross"
	FieldSplitPaid  = "spaid"
	FieldSplitBill  = "sbill"
)

// splitSwitches maps them to a new expense's.
var splitSwitches = map[string]string{FieldSplitGross: FieldGross, FieldSplitPaid: FieldPaid, FieldSplitBill: FieldBill}

// PartField names slot i's field: "s2.vendor".
func PartField(i int, name string) string { return "s" + strconv.Itoa(i) + "." + name }

func splitFields() []string {
	out := []string{FieldSplitGross, FieldSplitPaid, FieldSplitBill}
	for i := range maxParts {
		for _, n := range partNames {
			out = append(out, PartField(i, n))
		}
	}
	return out
}

// SplitPart is one slot of the desk: a scan or an expense found, or an
// empty one for a part without a scan.
type SplitPart struct {
	Scan    *receipts.Candidate
	Expense *sources.NinjaExpense
	Draft   metrics.ExpenseDraft
	Amount  float64
	Day     string // the invoice's
	Number  string
	Note    string
	Order   bool // the scan names the booking's order number: ticked
}

// Blank says whether the slot is empty: a part without a scan.
func (s SplitPart) Blank() bool { return s.Scan == nil && s.Expense == nil }

// splitPool are the free scans a split may take: the period's and
// splitDays around it, within the period's year (receipts.Unbooked
// reads one year).
func (b book) splitPool(ctx context.Context, d *sql.DB, who *access.Principal, s metrics.VATSpan) []receipts.Candidate {
	if b.scanSlot == 0 {
		return nil
	}
	from := s.Start.AddDate(0, 0, -splitDays)
	if from.Year() != s.Start.Year() {
		from = time.Date(s.Start.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	scans, _ := receipts.Unbooked(ctx, d, who, b.conn.ID, from, s.End.AddDate(0, 0, splitDays))
	return scans
}

// splitOf offers a debit's parts: scans and unlinked expenses near it
// and smaller than it, those naming its order number first, then the
// nearest; empty slots fill the rest. Order hits that make up the
// booking propose the split.
func (b book) splitOf(c *Case, pool []receipts.Candidate, vendorEuer map[string]string) {
	day, ok := metrics.ParseDay(c.Txn.Date)
	if !c.Txn.Debit || !ok || c.Action == ActionFill || c.Action == ActionReceipt {
		return
	}
	apart := func(v string) float64 {
		x, ok := metrics.ParseDay(v)
		if !ok {
			return math.Inf(1)
		}
		return math.Abs(x.Sub(day).Hours()) / hoursPerDay
	}
	fits := func(amount float64, v string) bool {
		return amount > 0 && amount < c.Txn.Amount-amountTol && apart(v) <= splitDays
	}

	orders := orderNumbers(c.Txn.Description)
	var parts []SplitPart
	for i := range pool {
		sc := &pool[i]
		if !fits(sc.Amount, sc.Doc.Day) {
			continue
		}
		p := SplitPart{Scan: sc, Draft: metrics.DraftOf(b.ninja, docTxn(sc.Doc, sc.Amount)), Amount: sc.Amount, Day: sc.Doc.Day,
			Note: strings.TrimSpace(sc.Doc.Title), Order: names(sc.Doc, orders)}
		p.Draft.Euer = vendorEuer[p.Draft.VendorKey]
		if b.numberSlot > 0 {
			p.Number = sc.Number
		}
		parts = append(parts, p)
	}
	taken := metrics.LinkedExpenses(b.ninja)
	for i, e := range b.ninja.Expenses {
		if e.TxnKey != "" || taken[e.Key] || !fits(e.Amount, e.Date) {
			continue
		}
		parts = append(parts, SplitPart{Expense: &b.ninja.Expenses[i], Amount: e.Amount, Day: e.Date,
			Draft: metrics.ExpenseDraft{Vendor: b.vendorName(e.VendorKey), VendorKey: e.VendorKey}})
	}
	slices.SortStableFunc(parts, func(x, y SplitPart) int {
		if x.Order && !y.Order {
			return -1
		}
		if y.Order && !x.Order {
			return 1
		}
		return cmp.Compare(apart(x.Day), apart(y.Day))
	})

	// At least one empty slot stays for a part without a scan.
	parts = parts[:min(len(parts), maxParts-1)]
	var hits int
	var sum float64
	for _, p := range parts {
		if p.Order {
			hits++
			sum += p.Amount
		}
	}
	for len(parts) < maxParts {
		parts = append(parts, SplitPart{Day: c.Txn.Date})
	}
	c.Split = parts
	if hits > 1 && math.Abs(sum-c.Txn.Amount) <= amountTol && (c.Action == metrics.ActionCreate || c.Action == metrics.ActionNone) {
		c.Action = ActionSplit
	}
}

// hoursPerDay turns a duration into days.
const hoursPerDay = 24

// orderPattern is an order number in a booking's text: digits, maybe
// grouped by dashes, e.g. Amazon's "302-4419871-5520347".
var orderPattern = regexp.MustCompile(`\d[\d-]{6,}\d`)

// minOrderDigits keeps dates and short numbers out: "2026-10-16" has 8.
const minOrderDigits = 10

// orderNumbers are the order numbers a booking's text names.
func orderNumbers(text string) []string {
	var out []string
	for _, m := range orderPattern.FindAllString(text, -1) {
		if len(m)-strings.Count(m, "-") >= minOrderDigits {
			out = append(out, m)
		}
	}
	return out
}

// names says whether a scan's text or title names one of the orders.
func names(doc sources.ReceiptDoc, orders []string) bool {
	return slices.ContainsFunc(orders, func(o string) bool {
		return strings.Contains(doc.Content, o) || strings.Contains(doc.Title, o)
	})
}

// split resolves a split: each slot used, the parts together the
// booking's amount, at least two parts and one of them an expense.
func (b book) split(s *workbench.Step, it Item) (Item, workbench.Problem) {
	if !it.Txn.Debit {
		return it, ProblemTarget
	}
	seen := splitSeen{scans: map[int64]bool{}, expenses: map[string]bool{}, taken: metrics.LinkedExpenses(b.ninja)}
	var sum float64
	expenses := 0
	for i := range maxParts {
		use := s.Field(PartField(i, FieldUse))
		if use != UseExpense && use != UsePrivate {
			for _, n := range partNames {
				delete(s.Fields, PartField(i, n))
			}
			continue
		}
		part, problem := b.part(s, i, it.Txn, &seen)
		if problem != workbench.ProblemNone {
			return it, problem
		}
		sum += part.Txn.Amount
		if !part.Private {
			expenses++
		}
		it.Parts = append(it.Parts, part)
	}
	if len(it.Parts) < 2 || expenses == 0 {
		return it, ProblemSplitParts
	}
	if math.Abs(sum-it.Txn.Amount) > amountTol {
		return it, ProblemSplitSum
	}
	it.Gross, it.Paid, it.Bill = s.Field(FieldSplitGross) != Off, s.Field(FieldSplitPaid) != Off, s.Field(FieldSplitBill) == On
	return it, workbench.ProblemNone
}

// splitSeen keeps a split's parts apart: a scan or an expense goes to
// one part, and an expense linked elsewhere to none.
type splitSeen struct {
	scans    map[int64]bool
	expenses map[string]bool
	taken    map[string]bool
}

// part resolves slot i: a private part, an expense found, or a new one
// (created, as for a whole booking, from the slot's fields).
func (b book) part(s *workbench.Step, i int, txn sources.NinjaBankTxn, seen *splitSeen) (Item, workbench.Problem) {
	field := func(name string) string { return strings.TrimSpace(s.Field(PartField(i, name))) }
	day := field(FieldDay)
	if _, ok := metrics.ParseDay(day); !ok {
		day = txn.Date
	}
	s.Fields[PartField(i, FieldDay)] = day
	part := Item{Txn: sources.NinjaBankTxn{Key: txn.Key, Date: day, Payer: txn.Payer, Description: txn.Description, Debit: true}}

	if key := field(FieldExpense); key != "" && field(FieldUse) == UseExpense {
		j := indexOf(b.ninja.Expenses, func(e sources.NinjaExpense) bool { return e.Key == key })
		if j < 0 || b.ninja.Expenses[j].TxnKey != "" || seen.taken[key] || seen.expenses[key] {
			return part, ProblemTarget
		}
		seen.expenses[key] = true
		e := &b.ninja.Expenses[j]
		part.Expense, part.Vendor = e, b.vendorName(e.VendorKey)
		part.Txn.Amount, part.Txn.Date = e.Amount, e.Date
		s.Fields[PartField(i, FieldAmount)], s.Fields[PartField(i, FieldDay)] = i18n.Decimal(e.Amount, b.num, 2), e.Date
		return part, workbench.ProblemNone
	}
	delete(s.Fields, PartField(i, FieldExpense))

	amount, err := i18n.ParseNum(field(FieldAmount), b.num)
	if err != nil || amount <= 0 {
		return part, ProblemAmount
	}
	part.Txn.Amount = amount
	if field(FieldUse) == UsePrivate {
		part.Private, part.Note = true, field(FieldNote)
		return part, workbench.ProblemNone
	}

	sub := workbench.Step{Case: s.Case, Action: string(metrics.ActionCreate), Fields: map[string]string{}}
	for _, n := range []string{FieldVendor, FieldCategory, FieldTax, FieldEuer, FieldNumber, FieldNote, FieldScan} {
		if v := field(n); v != "" {
			sub.Fields[n] = v
		}
	}
	for from, to := range splitSwitches {
		if v := s.Field(from); v != "" {
			sub.Fields[to] = v
		}
	}
	part, problem := b.created(&sub, part)
	s.Fields[PartField(i, FieldTax)] = sub.Fields[FieldTax]
	part.CatKey = sub.Field(FieldCategory)
	if part.Scan != 0 && seen.scans[part.Scan] {
		return part, ProblemTarget
	}
	if part.Scan != 0 {
		seen.scans[part.Scan] = true
	}
	return part, problem
}

// expenseChecks gives an existing expense to the first step that links
// it (a link or a split's part); a later one is refused, as Invoice
// Ninja would skip the expense and book the transaction without it.
func expenseChecks(steps []Planned) {
	taken := map[string]bool{}
	for i := range steps {
		p := &steps[i]
		if p.Problem != workbench.ProblemNone {
			continue
		}
		var keys []string
		switch p.Action {
		case string(metrics.ActionLink):
			if p.Item.Expense != nil {
				keys = append(keys, p.Item.Expense.Key)
			}
		case string(ActionSplit):
			for _, part := range p.Item.Parts {
				if part.Expense != nil {
					keys = append(keys, part.Expense.Key)
				}
			}
		}
		if slices.ContainsFunc(keys, func(k string) bool { return taken[k] }) {
			p.Problem = ProblemTarget
			continue
		}
		for _, k := range keys {
			taken[k] = true
		}
	}
}

// splitScans checks a split's scans: each still free (receipts.Unbooked
// of the booking's year) and not taken by an earlier part or step; one
// that is not is dropped, as for a whole booking.
func splitScans(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, p *Planned, taken map[int64]bool) {
	day, _ := metrics.ParseDay(p.Item.Txn.Date)
	year := time.Date(day.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	free, err := receipts.Unbooked(ctx, d, who, connID, year, year.AddDate(1, 0, -1))
	if err != nil {
		return // Paperless unreachable: linking reports it
	}
	j := 0
	for i := range maxParts {
		if s := p.Field(PartField(i, FieldUse)); s != UseExpense && s != UsePrivate {
			continue
		}
		part := &p.Item.Parts[j]
		j++
		if part.Scan == 0 {
			continue
		}
		k := slices.IndexFunc(free, func(c receipts.Candidate) bool { return c.Doc.ID == part.Scan })
		if k >= 0 && !taken[part.Scan] {
			part.ScanTitle = free[k].Doc.Title
			taken[part.Scan] = true
			continue
		}
		part.Scan = 0
		delete(p.Fields, PartField(i, FieldScan))
	}
}

// split writes a split: the new expenses, each with its scan, then one
// link of all to the booking. A part refused after others were
// created leaves those as expenses without a booking; the next split of
// it offers them.
func (w *writer) split(ctx context.Context, p Planned) (string, error) {
	it := p.Item
	var keys, numbers []string
	made, refused := 0, false
	for _, part := range it.Parts {
		if part.Private {
			continue
		}
		if part.Expense != nil {
			keys, numbers = append(keys, part.Expense.Key), append(numbers, part.Expense.Number)
			continue
		}
		key, number, err := w.expense(ctx, part, part.CatKey, it.Txn.Date)
		if err != nil && made > 0 {
			return joinNumbers(numbers), workbench.Partly{Target: joinNumbers(numbers), Reason: "vatcheck.problem_split_partly"}
		}
		if err != nil {
			return "", err
		}
		keys, numbers = append(keys, key), append(numbers, number)
		made++

		// Its scan at once, both sides: should a later part fail, the
		// scan is no longer free and the next split offers the expense.
		if part.Scan != 0 && w.scanLink(ctx, key, part.Scan) != nil {
			refused = true
		}
	}

	// Booked without every expense (one taken meanwhile) is booked; the
	// step says what is missing.
	target := joinNumbers(numbers)
	err := outbound.NinjaBankLink(ctx, w.to, it.Txn.Key, outbound.NinjaBankTarget{Expenses: keys})
	if err != nil && made == 0 && !errors.Is(err, outbound.ErrPartlyLinked) {
		return target, err
	}
	if err != nil {
		return target, workbench.Partly{Target: target, Reason: w.Reason(err)}
	}
	if refused {
		return target, workbench.Partly{Target: target, Reason: "vatcheck.problem_scan"}
	}
	return target, nil
}

// scanLink links a new expense's scan (receipts.Link).
func (w *writer) scanLink(ctx context.Context, key string, scan int64) error {
	if w.linkScan != nil {
		return w.linkScan(ctx, key, scan)
	}
	_, err := receipts.Link(ctx, w.d, w.who, key, []int64{scan}, w.ip)
	return err
}

func joinNumbers(numbers []string) string { return strings.Join(numbers, ", ") }
