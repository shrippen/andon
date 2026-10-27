package metrics

// VAT check: Invoice Ninja's bank transactions of one VAT period against
// its expenses and invoices, with the same booking in Sure beside each.
//
//	open debit  ─► expense, same amount, near, not linked yet    → link
//	            └► none: vendor, category and tax from the vendor's
//	               last expense, else Sure's merchant and category → create
//	open credit ─► open invoice: its number in the text, or the
//	               same balance and the client's name             → book
//	            └► a payment of the same amount booked already
//	               (by hand, from /billing), no transaction yet   → link
//
// Accounts are business or private as the user named them (unnamed
// count as business, but pair in no transfer). A debit from
// a private account is proposed only for a vendor with expenses (the
// train ticket paid privately); the rest waits, set apart. Incomes on
// private accounts and transactions archived in Invoice Ninja drop out.
// A mixed account's debits are treated like the private ones, its
// incomes like the business ones. A transfer account (card, PayPal)
// books its purchases; its settlement is a transfer, between marked
// accounts only (otherSides):
//
//	Giro  −120 "Kartenabrechnung" ─┐ same amount, a few days apart
//	Karte +120 "Zahlung"          ─┘ → both archived, nothing to book
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
	ActionPayment BankAction = "payment" // link a payment booked already
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
	Sure     *sources.BankTxn       // the same booking in Sure
	Expenses []sources.NinjaExpense // unlinked, same amount, best first
	Invoice  *sources.NinjaInvoice  // the open invoice it pays
	ByNumber bool                   // the text names the invoice's number
	Payment  *sources.NinjaPayment  // the payment booked for it already
	Draft    ExpenseDraft
	Action   BankAction
	Private  bool      // paid from an account with private bookings (private, mixed)
	Mixed    bool      // on a mixed account
	Transfer *Transfer // money moved to or from another own account
}

// Transfer is the other side of a move between own accounts: the same
// amount the other way on a private or transfer account (from one, on
// any other), in Invoice Ninja's feed or in Sure. Nothing to book:
// Andon proposes to archive it.
type Transfer struct {
	Account, Date string
}

// Waiting says whether the case belongs to the open work: business, or
// private with an expense to book (a known vendor).
func (c BankCase) Waiting() bool {
	return !c.Private || c.Action == ActionLink || c.Action == ActionCreate
}

// VATInputs is what the check reads.
type VATInputs struct {
	Ninja      *sources.NinjaDataset
	Sure       *sources.BankData // nil without Sure
	Start, End time.Time
	Method     string // ist, soll
	Today      time.Time
}

const (
	expenseWindow = 10 // days between booking and expense date
	bankWindow    = 4  // days between the same booking in two feeds
)

// BankCases lists the open transactions of the period, oldest first.
// Every expense, invoice and Sure booking is proposed once.
func BankCases(in VATInputs) []BankCase {
	if in.Ninja == nil {
		return nil
	}
	taken := LinkedExpenses(in.Ninja)
	usedSure, usedInvoice, usedPayment := map[string]bool{}, map[int64]bool{}, linkedPayments(in.Ninja)
	var out []BankCase
	for _, t := range periodTxns(in) {
		private := t.Role == sources.RolePrivate
		if t.Status == sources.BankDone || t.Archived || (private && !t.Debit) {
			continue
		}
		mixed := t.Role == sources.RoleMixed
		c := BankCase{Txn: t, Sure: sureTwin(in.Sure, t, usedSure), Private: private || (mixed && t.Debit), Mixed: mixed}
		if t.Debit {
			c.Expenses = expenseFits(in.Ninja, t, taken)
			c.Draft = draftOf(in.Ninja, t, c.Sure)
			c.Action = ActionCreate
			if len(c.Expenses) > 0 {
				c.Action = ActionLink
				taken[c.Expenses[0].Key] = true
			}
			if c.Private && c.Action == ActionCreate && c.Draft.From != FromHistory {
				c.Action = ActionNone
			}
		} else {
			c.Invoice, c.ByNumber = invoiceFit(in.Ninja, t, usedInvoice)
			if c.Invoice != nil {
				c.Action = ActionInvoice
				usedInvoice[c.Invoice.ID] = true
			} else if c.Payment = paymentFit(in.Ninja, t, usedPayment); c.Payment != nil {
				c.Action = ActionPayment
				usedPayment[c.Payment.Key] = true
			}
		}

		out = append(out, c)
	}
	pairTransfers(in, out)
	return out
}

// pairable: nothing to book for c, so it may be a transfer's side.
func pairable(c BankCase) bool {
	return c.Transfer == nil && (c.Action == ActionNone || (c.Action == ActionCreate && c.Draft.From != FromHistory))
}

// searches: c looks for its other side. A private or mixed account's
// debit does not (it is spending until shown otherwise), but is found as
// the side of a deposit or a card settlement.
func searches(c BankCase) bool { return !c.Private && pairable(c) }

// pairTransfers marks the transfers among the cases, once both sides
// know what else they could be: a pair of the period is decided once and
// set on both sides; a case without one may pair with a booking outside
// the period or only in Sure.
func pairTransfers(in VATInputs, cases []BankCase) {
	used := map[string]bool{}
	for i := range cases {
		c := &cases[i]
		if !searches(*c) {
			continue
		}
		if j := partnerOf(cases, i); j >= 0 {
			u := &cases[j]
			c.Transfer, u.Transfer = &Transfer{Account: u.Txn.Account, Date: u.Txn.Date}, &Transfer{Account: c.Txn.Account, Date: c.Txn.Date}
			c.Action, u.Action = ActionSkip, ActionSkip
			continue
		}
		if c.Transfer = transferOf(in, cases, c.Txn, used); c.Transfer != nil {
			c.Action = ActionSkip
		}
	}
}

// partnerOf is the open case pairing with cases[i], -1 none.
func partnerOf(cases []BankCase, i int) int {
	t := cases[i].Txn
	sides := otherSides(t)
	for j, u := range cases {
		if j != i && pairable(u) && pairs(t, sides, u.Txn) {
			return j
		}
	}
	return -1
}

// pairs: u is t's other side by role, direction, amount and date.
func pairs(t sources.NinjaBankTxn, sides []sources.AccountRole, u sources.NinjaBankTxn) bool {
	day, ok := ParseDay(t.Date)
	return ok && slices.Contains(sides, u.Role) && u.Debit != t.Debit && abs(u.Amount-t.Amount) <= amountTol && daysApart(u.Date, day) <= bankWindow
}

// transferOf finds t's other side outside the period's cases: the
// opposite booking of the same amount a few days apart, in the feed or
// in Sure, that nothing else explains (a customer's payment of the same
// amount is no card settlement).
func transferOf(in VATInputs, cases []BankCase, t sources.NinjaBankTxn, used map[string]bool) *Transfer {
	day, ok := ParseDay(t.Date)
	if !ok {
		return nil
	}
	sides := otherSides(t)
	for _, u := range in.Ninja.BankTxns {
		if !pairs(t, sides, u) || used["n"+u.Key] {
			continue
		}
		// A case of the period was weighed by partnerOf.
		if slices.ContainsFunc(cases, func(c BankCase) bool { return c.Txn.Key == u.Key }) || explained(in.Ninja, u) {
			continue
		}
		used["n"+u.Key] = true
		return &Transfer{Account: u.Account, Date: u.Date}
	}
	if in.Sure == nil {
		return nil
	}
	for _, s := range in.Sure.Transactions {
		// Sure's outflows are negative: a debit here is an inflow there.
		if !slices.Contains(sides, s.Role) || (s.Amount > 0) != t.Debit || used["s"+s.ID] || abs(abs(s.Amount)-t.Amount) > amountTol || daysApart(s.Date, day) > bankWindow {
			continue
		}
		// A booking the feed has too was weighed there, booked or not.
		twin := sources.NinjaBankTxn{Date: s.Date, Payer: s.Name, Amount: abs(s.Amount), Debit: s.Amount < 0, Role: s.Role}
		if inFeed(in.Ninja, twin) || explained(in.Ninja, twin) {
			continue
		}
		used["s"+s.ID] = true
		return &Transfer{Account: s.Account, Date: s.Date}
	}
	return nil
}

// explained: u is booked, or an expense, invoice or payment fits it.
func explained(n *sources.NinjaDataset, u sources.NinjaBankTxn) bool {
	if u.Status == sources.BankDone {
		return true
	}
	if u.Debit {
		return len(expenseFits(n, u, LinkedExpenses(n))) > 0
	}
	inv, _ := invoiceFit(n, u, nil)
	return inv != nil || paymentFit(n, u, linkedPayments(n)) != nil
}

// otherSides are the roles t's other side may have as a transfer. Both
// accounts must be marked (an account without a role pairs with none),
// and the rule holds both ways:
//
//	transfer +  ◄── business, mixed, private −   (card settlement)
//	transfer −  ──► business, mixed +            (payout, PayPal → giro)
//	business, mixed ◄─► private                  (withdrawal, deposit)
func otherSides(t sources.NinjaBankTxn) []sources.AccountRole {
	switch {
	case t.Role == sources.RoleTransfer && t.Debit:
		return []sources.AccountRole{sources.RoleBusiness, sources.RoleMixed}
	case t.Role == sources.RoleTransfer:
		return []sources.AccountRole{sources.RoleBusiness, sources.RoleMixed, sources.RolePrivate}
	case t.Role == sources.RoleUnknown:
		return nil
	}
	return []sources.AccountRole{sources.RolePrivate, sources.RoleTransfer}
}

// inFeed: the feed holds u's booking, same role and direction, the same
// amount a few days apart.
func inFeed(n *sources.NinjaDataset, u sources.NinjaBankTxn) bool {
	day, ok := ParseDay(u.Date)
	if !ok {
		return false
	}
	return slices.ContainsFunc(n.BankTxns, func(t sources.NinjaBankTxn) bool {
		return t.Role == u.Role && t.Debit == u.Debit && abs(t.Amount-u.Amount) <= amountTol && daysApart(t.Date, day) <= bankWindow
	})
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

// LinkedPayments are the payments a bank transaction holds already.
func LinkedPayments(n *sources.NinjaDataset) map[string]bool { return linkedPayments(n) }

func linkedPayments(n *sources.NinjaDataset) map[string]bool {
	out := map[string]bool{}
	for _, t := range n.BankTxns {
		if t.PaymentKey != "" {
			out[t.PaymentKey] = true
		}
	}
	return out
}

// paymentFit is a payment of the same amount near the income that no
// transaction holds: booked by hand, or on /billing from Sure. The one
// naming the payer's client first.
func paymentFit(n *sources.NinjaDataset, t sources.NinjaBankTxn, used map[string]bool) *sources.NinjaPayment {
	day, ok := ParseDay(t.Date)
	if !ok {
		return nil
	}
	clients := ninjaClientNames(n)
	var best *sources.NinjaPayment
	for i := range n.Payments {
		p := &n.Payments[i]
		if p.Key == "" || used[p.Key] || abs(p.Amount-t.Amount) > amountTol || daysApart(p.Date, day) > expenseWindow {
			continue
		}
		if best == nil || namesClient(t.Payer+" "+t.Description, clients[p.ClientID]) {
			best = p
		}
	}
	return best
}

// FeedTwin is the open income of the feed for a bank booking: same
// amount, a few days apart. /billing books through it, so the feed and
// the payment agree.
func FeedTwin(n *sources.NinjaDataset, amount float64, day time.Time) (sources.NinjaBankTxn, bool) {
	if n == nil {
		return sources.NinjaBankTxn{}, false
	}
	for _, t := range n.BankTxns {
		if !t.Debit && t.Status != sources.BankDone && !t.Archived && abs(t.Amount-amount) <= amountTol && daysApart(t.Date, day) <= bankWindow {
			return t, true
		}
	}
	return sources.NinjaBankTxn{}, false
}

// sureTwin is the same booking in Sure: same amount and direction, a few
// days apart, the one naming the payee first.
func sureTwin(sure *sources.BankData, t sources.NinjaBankTxn, used map[string]bool) *sources.BankTxn {
	if sure == nil {
		return nil
	}
	day, ok := ParseDay(t.Date)
	if !ok {
		return nil
	}
	var best *sources.BankTxn
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
func draftOf(n *sources.NinjaDataset, t sources.NinjaBankTxn, sure *sources.BankTxn) ExpenseDraft {
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
	Open, Proposed int                    // open business transactions, of them with a proposal
	FeedTo         string                 // newest transaction of the feed
	FeedLate       bool                   // the feed ends before the period
	NoTax          []sources.NinjaExpense // without tax, while the vendor's others have it
	SureOnly       []sources.BankTxn      // business bookings Ninja's feed lacks
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
	for _, bc := range cases {
		if !bc.Waiting() {
			continue
		}
		c.Open++
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
	c.SureOnly = sureOnly(in)
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

// twinInFeed reports whether the feed has the Sure booking.
func twinInFeed(n *sources.NinjaDataset, s sources.BankTxn) bool {
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

// sureOnly are the period's bookings of Sure's business accounts that
// the feed lacks: Invoice Ninja missed them. Without roles there is no
// telling business from private: nothing.
func sureOnly(in VATInputs) []sources.BankTxn {
	if in.Sure == nil || !in.Sure.Classified() {
		return nil
	}
	var out []sources.BankTxn
	for _, s := range in.Sure.Transactions {
		d, ok := ParseDay(s.Date)
		if !ok || d.Before(in.Start) || d.After(in.End) || !slices.Contains(sources.BusinessRoles, s.Role) {
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

// VATSpanAt is the period holding day: the one a booking is declared in.
func VATSpanAt(day time.Time, interval string) VATSpan {
	return VATSpans(day, interval, 1)[0]
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

// SharedBookings counts, per Invoice Ninja bank account and Sure account,
// the bookings both feeds have (same amount and direction, a few days
// apart): the same real account shares many.
//
//	{"Kx9": {"a1": 14, "a2": 0}}
func SharedBookings(n *sources.NinjaDataset, s *sources.BankData) map[string]map[string]int {
	out := map[string]map[string]int{}
	if n == nil || s == nil {
		return out
	}
	for _, t := range n.BankTxns {
		day, ok := ParseDay(t.Date)
		if !ok || t.AccountKey == "" {
			continue
		}
		for _, b := range s.Transactions {
			if b.AccountID == "" || (b.Amount < 0) != t.Debit || abs(abs(b.Amount)-t.Amount) > amountTol || daysApart(b.Date, day) > bankWindow {
				continue
			}
			if out[t.AccountKey] == nil {
				out[t.AccountKey] = map[string]int{}
			}
			out[t.AccountKey][b.AccountID]++
		}
	}
	return out
}

// VendorGroup is vendors of one name told apart only by case or legal
// form ("Nordhost", "NORDHOST GmbH"): the one with most expenses first.
type VendorGroup struct {
	Vendors  []sources.NinjaVendor
	Expenses map[string]int // per vendor key, in the dataset's window
}

// VendorDuplicates groups vendors whose names start with the same word.
func VendorDuplicates(n *sources.NinjaDataset) []VendorGroup {
	if n == nil {
		return nil
	}
	count := map[string]int{}
	for _, e := range n.Expenses {
		count[e.VendorKey]++
	}
	byWord := map[string][]sources.NinjaVendor{}
	var words []string
	for _, v := range n.Vendors {
		t := nameTokens(v.Name)
		if len(t) == 0 || v.Archived {
			continue
		}
		if _, seen := byWord[t[0]]; !seen {
			words = append(words, t[0])
		}
		byWord[t[0]] = append(byWord[t[0]], v)
	}
	var out []VendorGroup
	for _, w := range words {
		list := byWord[w]
		if len(list) < 2 {
			continue
		}
		slices.SortStableFunc(list, func(a, b sources.NinjaVendor) int {
			if c := cmp.Compare(count[b.Key], count[a.Key]); c != 0 {
				return c
			}
			return cmp.Compare(a.Name, b.Name)
		})
		g := VendorGroup{Vendors: list, Expenses: map[string]int{}}
		for _, v := range list {
			g.Expenses[v.Key] = count[v.Key]
		}
		out = append(out, g)
	}
	return out
}
