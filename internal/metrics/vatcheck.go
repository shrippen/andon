package metrics

// VAT check: Invoice Ninja's bank transactions of one VAT period against
// its expenses and invoices, with the same booking in Sure beside each.
//
//	open debit  ─► expense, same amount, near, not linked yet    → link
//	            └► none: vendor, category and tax from the vendor's
//	               last expense, else Sure's merchant and category → create
//	open credit ─► open invoice: its number in the text, or the
//	               same balance and the client's name             → book
//
// VATCheckOf says what must hold before the return goes out.

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

// BankAction is what a case proposes.
type BankAction string

const (
	ActionNone    BankAction = ""
	ActionLink    BankAction = "link"    // link an existing expense
	ActionCreate  BankAction = "create"  // create the expense, then link
	ActionInvoice BankAction = "invoice" // book the payment of an invoice
	ActionSkip    BankAction = "skip"    // set aside: private, nothing to book
)

// DraftFrom says where a new expense's fields come from.
type DraftFrom string

const (
	FromNothing DraftFrom = ""
	FromHistory DraftFrom = "history" // the vendor's last expense
	FromSure    DraftFrom = "sure"    // Sure's merchant and category
	FromModel   DraftFrom = "model"   // the language model, on request
)

// ExpenseDraft is a new expense for an open debit. VendorKey or
// CategoryKey "" means none known: the vendor is created by name.
type ExpenseDraft struct {
	Vendor, VendorKey     string
	Category, CategoryKey string
	TaxName               string
	TaxRate               float64
	TaxKnown              bool
	From                  DraftFrom
}

// BankCase is one open transaction with what it could be.
type BankCase struct {
	Txn      sources.NinjaBankTxn
	Sure     *sources.SureTxn       // the same booking in Sure
	Expenses []sources.NinjaExpense // unlinked, same amount, best first
	Invoice  *sources.NinjaInvoice  // the open invoice it pays
	ByNumber bool                   // the text names the invoice's number
	Draft    ExpenseDraft
	Action   BankAction
}

// VATInputs is what the check reads; Skip holds transactions the user
// set aside (private).
type VATInputs struct {
	Ninja      *sources.NinjaDataset
	Sure       *sources.SureDataset // nil without Sure
	Start, End time.Time
	Skip       map[string]bool
	Method     string // ist, soll
	Today      time.Time
}

const (
	expenseWindow = 10 // days between booking and expense date
	bankWindow    = 4  // days between the same booking in two feeds
	sharedMin     = 2  // shared bookings that make a Sure account the feed's
)

// BankCases lists the open transactions of the period, oldest first.
// Every expense, invoice and Sure booking is proposed once.
func BankCases(in VATInputs) []BankCase {
	if in.Ninja == nil {
		return nil
	}
	taken := LinkedExpenses(in.Ninja)
	usedSure, usedInvoice := map[string]bool{}, map[int64]bool{}
	var out []BankCase
	for _, t := range periodTxns(in) {
		if t.Status == sources.BankDone || in.Skip[t.Key] {
			continue
		}
		c := BankCase{Txn: t, Sure: sureTwin(in.Sure, t, usedSure)}
		if t.Debit {
			c.Expenses = expenseFits(in.Ninja, t, taken)
			c.Draft = draftOf(in.Ninja, t, c.Sure)
			c.Action = ActionCreate
			if len(c.Expenses) > 0 {
				c.Action = ActionLink
				taken[c.Expenses[0].Key] = true
			}
		} else {
			c.Invoice, c.ByNumber = invoiceFit(in.Ninja, t, usedInvoice)
			if c.Invoice != nil {
				c.Action = ActionInvoice
				usedInvoice[c.Invoice.ID] = true
			}
		}
		out = append(out, c)
	}
	return out
}

// periodTxns are the period's transactions, oldest first.
func periodTxns(in VATInputs) []sources.NinjaBankTxn {
	var out []sources.NinjaBankTxn
	for _, t := range in.Ninja.BankTxns {
		if d, ok := ParseDay(t.Date); ok && !d.Before(in.Start) && !d.After(in.End) {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b sources.NinjaBankTxn) int { return cmp.Compare(a.Date, b.Date) })
	return out
}

// LinkedExpenses are the expenses a bank transaction already holds.
func LinkedExpenses(n *sources.NinjaDataset) map[string]bool {
	out := map[string]bool{}
	for _, e := range n.Expenses {
		if e.TxnKey != "" {
			out[e.Key] = true
		}
	}
	for _, t := range n.BankTxns {
		for _, k := range t.ExpenseKeys {
			out[k] = true
		}
	}
	return out
}

func daysApart(a string, b time.Time) float64 {
	d, ok := ParseDay(a)
	if !ok {
		return 1e9
	}
	return abs(d.Sub(b).Hours() / hoursPerDay)
}

// expenseFits are the unlinked expenses of the same amount near the
// booking: those naming the payee first, then the nearest.
func expenseFits(n *sources.NinjaDataset, t sources.NinjaBankTxn, taken map[string]bool) []sources.NinjaExpense {
	day, ok := ParseDay(t.Date)
	if !ok {
		return nil
	}
	names := vendorNames(n)
	text := t.Payer + " " + t.Description
	var out []sources.NinjaExpense
	for _, e := range n.Expenses {
		if e.Key == "" || taken[e.Key] || abs(e.Amount-t.Amount) > amountTol || daysApart(e.Date, day) > expenseWindow {
			continue
		}
		out = append(out, e)
	}
	named := func(e sources.NinjaExpense) bool {
		return sameThing(names[e.VendorKey], text) || sameThing(t.Payer, e.Notes)
	}
	slices.SortStableFunc(out, func(a, b sources.NinjaExpense) int {
		if na, nb := named(a), named(b); na != nb {
			if na {
				return -1
			}
			return 1
		}
		return cmp.Compare(daysApart(a.Date, day), daysApart(b.Date, day))
	})
	return out
}

func vendorNames(n *sources.NinjaDataset) map[string]string {
	out := make(map[string]string, len(n.Vendors))
	for _, v := range n.Vendors {
		out[v.Key] = v.Name
	}
	return out
}

// invoiceFit is the open invoice a credit pays: its number in the text,
// else the same balance from a payee named like the client.
func invoiceFit(n *sources.NinjaDataset, t sources.NinjaBankTxn, used map[int64]bool) (*sources.NinjaInvoice, bool) {
	text := compact(t.Payer + " " + t.Description)
	clients := ninjaClientNames(n)
	var byName *sources.NinjaInvoice
	for i := range n.Invoices {
		inv := &n.Invoices[i]
		if !isOpen(inv.Status) || inv.Balance <= 0 || used[inv.ID] {
			continue
		}
		if number := compact(inv.Number); number != "" && strings.Contains(text, number) {
			return inv, true
		}
		if byName == nil && abs(inv.Balance-t.Amount) <= amountTol && namesClient(t.Payer+" "+t.Description, clients[inv.ClientID]) {
			byName = inv
		}
	}
	return byName, false
}

// sureTwin is the same booking in Sure: same amount and direction, a few
// days apart, the one naming the payee first.
func sureTwin(sure *sources.SureDataset, t sources.NinjaBankTxn, used map[string]bool) *sources.SureTxn {
	if sure == nil {
		return nil
	}
	day, ok := ParseDay(t.Date)
	if !ok {
		return nil
	}
	var best *sources.SureTxn
	for i := range sure.Transactions {
		s := &sure.Transactions[i]
		if used[s.ID] || (s.Amount < 0) != t.Debit || abs(abs(s.Amount)-t.Amount) > amountTol || daysApart(s.Date, day) > bankWindow {
			continue
		}
		if best == nil || sameThing(t.Payer, s.Name+" "+s.Merchant) {
			best = s
		}
	}
	if best != nil {
		used[best.ID] = true
	}
	return best
}

// draftOf fills a new expense: a known vendor (its first word is a word
// of the booking: "Mietwagen Nord" is not "Nordhost") brings its last
// category and tax; otherwise Sure's merchant and category help.
func draftOf(n *sources.NinjaDataset, t sources.NinjaBankTxn, sure *sources.SureTxn) ExpenseDraft {
	var d ExpenseDraft
	text := t.Payer + " " + t.Description
	if sure != nil {
		text += " " + sure.Merchant
	}
	for _, v := range n.Vendors {
		if strings.EqualFold(v.Name, t.Payer) || sameThing(v.Name, text) {
			d.Vendor, d.VendorKey = v.Name, v.Key
			break
		}
	}
	if last, ok := lastExpense(n, d.VendorKey); ok {
		d.CategoryKey, d.TaxName, d.TaxRate, d.TaxKnown, d.From = last.CategoryKey, last.TaxName, last.TaxRate, true, FromHistory
		d.Category = categoryName(n, d.CategoryKey)
		return d
	}
	if d.Vendor == "" {
		d.Vendor = t.Payer
		if sure != nil && sure.Merchant != "" {
			d.Vendor, d.From = sure.Merchant, FromSure
		}
	}
	if sure == nil || sure.Category == "" {
		return d
	}
	for _, c := range n.Categories {
		if strings.EqualFold(c.Name, sure.Category) {
			d.Category, d.CategoryKey, d.From = c.Name, c.Key, FromSure
		}
	}
	return d
}

// lastExpense is the vendor's newest expense.
func lastExpense(n *sources.NinjaDataset, vendorKey string) (sources.NinjaExpense, bool) {
	var last sources.NinjaExpense
	found := false
	for _, e := range n.Expenses {
		if vendorKey != "" && e.VendorKey == vendorKey && (!found || e.Date > last.Date) {
			last, found = e, true
		}
	}
	return last, found
}

func categoryName(n *sources.NinjaDataset, key string) string {
	for _, c := range n.Categories {
		if c.Key == key {
			return c.Name
		}
	}
	return ""
}

// ── the check before the return ──

// VATCheck is what must hold before the period's return goes out.
type VATCheck struct {
	Open, Proposed int                    // open transactions, of them with a proposal
	FeedTo         string                 // newest transaction of the feed
	FeedLate       bool                   // the feed ends before the period
	NoTax          []sources.NinjaExpense // without tax, while the vendor's others have it
	SureOnly       []sources.SureTxn      // bookings of the feed's accounts Ninja lacks
	Accounts       []string               // Sure accounts the feed shares
	Output, Input  float64
	Due            float64
}

// Ready says whether nothing is left to do.
func (c VATCheck) Ready() bool {
	return c.Open == 0 && !c.FeedLate && len(c.NoTax) == 0 && len(c.SureOnly) == 0
}

// VATCheckOf checks the period of in, with its cases.
func VATCheckOf(in VATInputs, cases []BankCase) VATCheck {
	var c VATCheck
	if in.Ninja == nil {
		return c
	}
	c.Open = len(cases)
	for _, bc := range cases {
		if bc.Action != ActionNone {
			c.Proposed++
		}
	}
	for _, t := range in.Ninja.BankTxns {
		c.FeedTo = max(c.FeedTo, t.Date)
	}
	periodOver := in.End.Before(in.Today)
	if feed, ok := ParseDay(c.FeedTo); periodOver && (!ok || feed.Before(in.End)) {
		c.FeedLate = true
	}
	c.NoTax = untaxed(in)
	c.Accounts = FeedAccounts(in.Sure, in.Ninja)
	c.SureOnly = sureOnly(in, c.Accounts)
	c.Output, c.Input = NinjaOutputVAT(in.Ninja, in.Start, in.End, in.Method), NinjaInputVAT(in.Ninja, in.Start, in.End)
	c.Due = round2(c.Output - c.Input)
	return c
}

// untaxed are the period's expenses without tax whose vendor usually
// has some: a rate forgotten.
func untaxed(in VATInputs) []sources.NinjaExpense {
	taxed := map[string]bool{}
	for _, e := range in.Ninja.Expenses {
		if e.Tax > 0 && e.VendorKey != "" {
			taxed[e.VendorKey] = true
		}
	}
	var out []sources.NinjaExpense
	for _, e := range in.Ninja.Expenses {
		d, ok := ParseDay(e.Date)
		if ok && !d.Before(in.Start) && !d.After(in.End) && e.Tax == 0 && taxed[e.VendorKey] {
			out = append(out, e)
		}
	}
	return out
}

// FeedAccounts are the Sure accounts whose bookings Invoice Ninja's bank
// feed also has: the business accounts, without asking for them.
func FeedAccounts(sure *sources.SureDataset, n *sources.NinjaDataset) []string {
	if sure == nil || n == nil || len(n.BankTxns) == 0 {
		return nil
	}
	shared := map[string]int{}
	for _, s := range sure.Transactions {
		if twinInFeed(n, s) {
			shared[s.Account]++
		}
	}
	var out []string
	for account, count := range shared {
		if count >= sharedMin && account != "" {
			out = append(out, account)
		}
	}
	slices.Sort(out)
	return out
}

// twinInFeed reports whether the feed has the Sure booking.
func twinInFeed(n *sources.NinjaDataset, s sources.SureTxn) bool {
	day, ok := ParseDay(s.Date)
	if !ok {
		return false
	}
	for _, t := range n.BankTxns {
		if (s.Amount < 0) == t.Debit && abs(abs(s.Amount)-t.Amount) <= amountTol && daysApart(t.Date, day) <= bankWindow {
			return true
		}
	}
	return false
}

// sureOnly are the period's bookings of the feed's accounts that the
// feed lacks: Invoice Ninja missed them.
func sureOnly(in VATInputs, accounts []string) []sources.SureTxn {
	if in.Sure == nil || len(accounts) == 0 {
		return nil
	}
	var out []sources.SureTxn
	for _, s := range in.Sure.Transactions {
		d, ok := ParseDay(s.Date)
		if !ok || d.Before(in.Start) || d.After(in.End) || !slices.Contains(accounts, s.Account) {
			continue
		}
		if !twinInFeed(in.Ninja, s) {
			out = append(out, s)
		}
	}
	return out
}

// ── periods ──

// VATSpan is one VAT period; Key names it in links ("2026-09", "2026-Q3").
type VATSpan struct {
	Key        string
	Start, End time.Time
}

// VATSpans are the n periods up to the one holding today, newest first.
func VATSpans(today time.Time, interval string, n int) []VATSpan {
	out := make([]VATSpan, 0, n)
	start, _ := NinjaVATPeriod(today, interval)
	step := 1
	if interval == "quarterly" {
		step = quarterMonths
	}
	for i := 0; i < n; i++ {
		s := AddMonths(start, -i*step)
		out = append(out, VATSpan{Key: spanKey(s, interval), Start: s, End: AddMonths(s, step).AddDate(0, 0, -1)})
	}
	return out
}

func spanKey(start time.Time, interval string) string {
	if interval == "quarterly" {
		return fmt.Sprintf("%d-Q%d", start.Year(), (int(start.Month())-1)/quarterMonths+1)
	}
	return start.Format("2006-01")
}

// VATSpanOf reads a key back; ok is false for anything else.
func VATSpanOf(key, interval string) (VATSpan, bool) {
	if interval == "quarterly" {
		year, q, ok := strings.Cut(key, "-Q")
		y, err1 := strconv.Atoi(year)
		n, err2 := strconv.Atoi(q)
		if !ok || err1 != nil || err2 != nil || n < 1 || n > 4 {
			return VATSpan{}, false
		}
		s := time.Date(y, time.Month((n-1)*quarterMonths+1), 1, 0, 0, 0, 0, time.UTC)
		return VATSpan{Key: key, Start: s, End: AddMonths(s, quarterMonths).AddDate(0, 0, -1)}, true
	}
	s, err := time.Parse("2006-01", key)
	if err != nil {
		return VATSpan{}, false
	}
	return VATSpan{Key: key, Start: s, End: AddMonths(s, 1).AddDate(0, 0, -1)}, true
}

// Quarter is the span's quarter of the year (1–4).
func (s VATSpan) Quarter() int { return (int(s.Start.Month())-1)/quarterMonths + 1 }
