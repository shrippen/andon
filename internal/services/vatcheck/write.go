package vatcheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

// Step is one booking the user asked for. Create uses Vendor, Category
// (a key) and TaxRate (Taxed false: no tax).
type Step struct {
	Txn      string
	Action   metrics.BankAction
	Expense  string // link: the expense key
	Invoice  string // invoice: the invoice key
	Payment  string // payment: the payment key
	Vendor   string
	Category string
	TaxRate  float64
	Taxed    bool
}

// Problem says why a step cannot be written.
type Problem string

const (
	ProblemNone     Problem = ""
	ProblemGone     Problem = "vatcheck.problem_gone"      // booked meanwhile
	ProblemTarget   Problem = "vatcheck.problem_target"    // expense or invoice no longer fits
	ProblemNoVendor Problem = "vatcheck.problem_no_vendor" // a new expense needs a vendor
)

// ErrGone means the transaction is not (or no longer) archived.
var ErrGone = errors.New("vatcheck.problem_gone")

// Planned is a step as it would be written.
type Planned struct {
	Step
	Txn       sources.NinjaBankTxn
	Expense   *sources.NinjaExpense
	Invoice   *sources.NinjaInvoice
	Payment   *sources.NinjaPayment
	NewVendor bool   // the vendor is created first
	Category  string // its name
	TaxName   string
	Tax       float64 // input VAT in the amount
	Problem   Problem
}

// Plan is the review: what Apply would write.
type Plan struct {
	Conn     int64
	Steps    []Planned
	Currency string
}

// Writes counts the steps that change Invoice Ninja; archiving a
// private transaction is one too.
func (p Plan) Writes() int {
	n := 0
	for _, s := range p.Steps {
		if s.Problem == ProblemNone {
			n++
		}
	}
	return n
}

// Review checks the steps against the current data and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, steps []Step) (Plan, error) {
	b, _, err := load(ctx, d, who, connID, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	return b.plan(steps)
}

func (b book) plan(steps []Step) (Plan, error) {
	if len(steps) == 0 {
		return Plan{}, ErrNothing
	}
	out := Plan{Conn: b.conn.ID, Currency: b.ninja.Currency}
	for _, s := range steps {
		out.Steps = append(out.Steps, b.planned(s))
	}
	return out, nil
}

// planned resolves one step; the transaction must still be open and
// its target must still fit.
func (b book) planned(s Step) Planned {
	p := Planned{Step: s}
	i := indexOf(b.ninja.BankTxns, func(t sources.NinjaBankTxn) bool { return t.Key == s.Txn })
	if i < 0 || b.ninja.BankTxns[i].Status == sources.BankDone {
		p.Problem = ProblemGone
		return p
	}
	p.Txn = b.ninja.BankTxns[i]
	taken := metrics.LinkedExpenses(b.ninja)
	switch s.Action {
	case metrics.ActionLink:
		j := indexOf(b.ninja.Expenses, func(e sources.NinjaExpense) bool { return e.Key == s.Expense })
		if j < 0 || taken[s.Expense] || math.Abs(b.ninja.Expenses[j].Amount-p.Txn.Amount) > amountTol {
			p.Problem = ProblemTarget
			return p
		}
		p.Expense = &b.ninja.Expenses[j]
	case metrics.ActionInvoice:
		// Ninja pays at most the balance: a part payment fits too.
		j := indexOf(b.ninja.Invoices, func(inv sources.NinjaInvoice) bool { return inv.Key == s.Invoice })
		if j < 0 || p.Txn.Debit || b.ninja.Invoices[j].Balance <= 0 || p.Txn.Amount > b.ninja.Invoices[j].Balance+amountTol {
			p.Problem = ProblemTarget
			return p
		}
		p.Invoice = &b.ninja.Invoices[j]
	case metrics.ActionPayment:
		j := indexOf(b.ninja.Payments, func(pay sources.NinjaPayment) bool { return pay.Key == s.Payment })
		if j < 0 || p.Txn.Debit || metrics.LinkedPayments(b.ninja)[s.Payment] || math.Abs(b.ninja.Payments[j].Amount-p.Txn.Amount) > amountTol {
			p.Problem = ProblemTarget
			return p
		}
		p.Payment = &b.ninja.Payments[j]
	case metrics.ActionCreate:
		p.Vendor = strings.TrimSpace(s.Vendor)
		if p.Vendor == "" || !p.Txn.Debit {
			p.Problem = ProblemNoVendor
			return p
		}
		p.NewVendor = b.vendorKey(p.Vendor) == ""
		for _, c := range b.ninja.Categories {
			if c.Key == s.Category {
				p.Category = c.Name
			}
		}
		if s.Taxed {
			p.TaxName = b.taxName(s.TaxRate)
			p.Tax = math.Round(p.Txn.Amount*s.TaxRate/(100+s.TaxRate)*cents) / cents
		}
	case metrics.ActionSkip:
	default:
		p.Problem = ProblemTarget
	}
	return p
}

const (
	amountTol = 0.01
	cents     = 100
	// defaultTaxName names a rate Invoice Ninja has no entry for.
	defaultTaxName = "MwSt."
)

func indexOf[T any](list []T, fit func(T) bool) int {
	for i, x := range list {
		if fit(x) {
			return i
		}
	}
	return -1
}

func (b book) vendorKey(name string) string {
	for _, v := range b.ninja.Vendors {
		if equalFold(v.Name, name) {
			return v.Key
		}
	}
	return ""
}

func (b book) taxName(rate float64) string {
	for _, r := range b.ninja.TaxRates {
		if r.Rate == rate {
			return r.Name
		}
	}
	return defaultTaxName
}

// Result is what Apply wrote.
type Result struct {
	Done, Skipped int
	Failed        []Failure
}

// Failure is a step Invoice Ninja refused, or one that no longer fit.
type Failure struct {
	Txn, Payer string
	Reason     string // i18n key or Invoice Ninja's message
}

// Apply writes the confirmed steps against fresh data; each is checked
// again, so a transaction booked meanwhile is not booked twice.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, steps []Step, ip string) (Result, error) {
	b, _, err := load(ctx, d, who, connID, svcdata.Force)
	if err != nil {
		return Result{}, err
	}
	if err := connections.Writable(d, who, b.conn.ID); err != nil {
		return Result{}, err
	}
	plan, err := b.plan(steps)
	if err != nil {
		return Result{}, err
	}
	if plan.Writes() > 0 && sources.IsDemo(b.conn.URL) {
		return Result{}, ErrDemo
	}
	token, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
	if err != nil {
		return Result{}, err
	}
	w := writer{to: outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}, book: b}

	var out Result
	for _, p := range plan.Steps {
		if p.Problem != ProblemNone {
			out.Failed = append(out.Failed, Failure{Txn: p.Step.Txn, Payer: p.Txn.Payer, Reason: string(p.Problem)})
			continue
		}
		target, err := w.write(ctx, p)
		if err != nil {
			out.Failed = append(out.Failed, Failure{Txn: p.Txn.Key, Payer: p.Txn.Payer, Reason: reasonOf(err)})
			continue
		}
		if p.Action == metrics.ActionSkip {
			out.Skipped++
		} else {
			out.Done++
		}
		_ = auditsvc.Log(d, &who.UserID, "vatcheck."+string(p.Action), target, ip,
			map[string]any{"txn": p.Txn.Key, "amount": fmt.Sprintf("%.2f", p.Txn.Amount)})
	}

	svcdata.Forget(b.conn.ID)
	err = saveState(d, who, func(s *state) { s.Conn = b.conn.ID })
	return out, err
}

// reasonOf is a refusal's text: a known one's key, else the message.
func reasonOf(err error) string {
	if errors.Is(err, outbound.ErrNotBooked) {
		return "vatcheck.problem_not_booked"
	}
	if errors.Is(err, outbound.ErrNotArchived) {
		return "vatcheck.problem_not_archived"
	}
	return err.Error()
}

// writer books steps in one Invoice Ninja.
type writer struct {
	to   outbound.Target
	book book
}

// write books one step and returns what it booked against (an expense
// or invoice number, the payee of an archived one).
func (w writer) write(ctx context.Context, p Planned) (string, error) {
	switch p.Action {
	case metrics.ActionSkip:
		return p.Txn.Payer, outbound.NinjaBankArchive(ctx, w.to, p.Txn.Key, outbound.BankArchiveOn)
	case metrics.ActionLink:
		return p.Expense.Number, outbound.NinjaBankLink(ctx, w.to, p.Txn.Key, outbound.NinjaBankTarget{Expense: p.Expense.Key})
	case metrics.ActionInvoice:
		return p.Invoice.Number, outbound.NinjaBankLink(ctx, w.to, p.Txn.Key, outbound.NinjaBankTarget{Invoices: []string{p.Invoice.Key}})
	case metrics.ActionPayment:
		return p.Payment.Number, outbound.NinjaBankLink(ctx, w.to, p.Txn.Key, outbound.NinjaBankTarget{Payment: p.Payment.Key})
	}

	// A new expense: vendor first if new, the expense with its tax, then
	// the link that marks the transaction booked.
	vendor := w.book.vendorKey(p.Vendor)
	if vendor == "" {
		key, err := outbound.NinjaVendorCreate(ctx, w.to, p.Vendor)
		if err != nil {
			return "", err
		}
		vendor = key
	}
	body := map[string]any{"amount": p.Txn.Amount, "date": p.Txn.Date, "payment_date": p.Txn.Date, "vendor_id": vendor,
		"public_notes": p.Txn.Description, "transaction_reference": p.Txn.Description, "uses_inclusive_taxes": true}
	if p.Step.Category != "" {
		body["category_id"] = p.Step.Category
	}
	if p.Taxed {
		body["tax_name1"], body["tax_rate1"] = p.TaxName, p.TaxRate
	}
	key, number, err := outbound.NinjaExpenseCreate(ctx, w.to, body)
	if err != nil {
		return "", err
	}
	if err := outbound.NinjaBankLink(ctx, w.to, p.Txn.Key, outbound.NinjaBankTarget{Expense: key}); err != nil {
		return number, fmt.Errorf("%s: %w", number, err)
	}
	return number, nil
}

// Restore brings a transaction archived as private back into the feed.
func Restore(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, txn string, ip string) error {
	b, _, err := load(ctx, d, who, connID, svcdata.Cached)
	if err != nil {
		return err
	}
	if err := connections.Writable(d, who, b.conn.ID); err != nil {
		return err
	}
	i := indexOf(b.ninja.BankTxns, func(t sources.NinjaBankTxn) bool { return t.Key == txn && t.Archived })
	if i < 0 {
		return ErrGone
	}
	if sources.IsDemo(b.conn.URL) {
		return ErrDemo
	}
	token, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
	if err != nil {
		return err
	}
	to := outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}
	if err := outbound.NinjaBankArchive(ctx, to, txn, outbound.BankArchiveOff); err != nil {
		return err
	}
	svcdata.Forget(b.conn.ID)
	return auditsvc.Log(d, &who.UserID, "vatcheck.restore", b.ninja.BankTxns[i].Payer, ip, map[string]any{"txn": txn})
}

// Role is one account's role as the form sends it.
type Role struct {
	ConnID int64
	Key    string
	Role   sources.AccountRole
}

// SaveRoles stores the roles that changed; the Verbund carries each to
// the linked accounts (verbund.SetRole).
func SaveRoles(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, roles []Role, ip string) error {
	b, _, err := load(ctx, d, who, connID, svcdata.Cached)
	if err != nil {
		return err
	}
	known := b.accounts()
	for _, r := range roles {
		i := indexOf(known, func(a Account) bool { return a.ConnID == r.ConnID && a.Key == r.Key })
		if i < 0 || known[i].Role == r.Role {
			continue
		}
		if err := verbund.SetRole(d, who, r.ConnID, r.Key, r.Role, ip); err != nil {
			return err
		}
	}
	return nil
}

// Guess asks the language model about the period's open debits and
// keeps its readings; it returns how many it answered.
func Guess(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, spanKey string, today time.Time) (int, error) {
	page, err := Open(ctx, d, who, connID, spanKey, today)
	if err != nil {
		return 0, err
	}
	var list []assist.Booking
	for _, c := range append(page.Cases, page.Apart...) {
		if !c.Txn.Debit || c.Action == metrics.ActionLink {
			continue
		}
		bk := assist.Booking{Key: c.Txn.Key, Date: c.Txn.Date, Payer: c.Txn.Payer, Text: c.Txn.Description, Amount: c.Txn.Amount, Debit: true}
		if c.Sure != nil {
			bk.SureMerchant, bk.SureCategory, bk.SureTags = c.Sure.Merchant, c.Sure.Category, c.Sure.Tags
		}
		list = append(list, bk)
	}
	if len(list) == 0 {
		return 0, nil
	}
	ledger := assist.Ledger{Vendors: page.Ledger.Vendors}
	for _, c := range page.Ledger.Categories {
		ledger.Categories = append(ledger.Categories, c.Name)
	}
	for _, r := range page.Ledger.TaxRates {
		ledger.TaxRates = append(ledger.TaxRates, r.Rate)
	}
	if !slices.Contains(ledger.TaxRates, 0) {
		ledger.TaxRates = append(ledger.TaxRates, 0)
	}

	guesses, err := assist.Classify(ctx, who.Locale, list, ledger)
	if err != nil {
		return 0, err
	}
	err = saveState(d, who, func(s *state) {
		s.Conn = page.Conn
		key := connKey(page.Conn)
		if s.Guesses[key] == nil {
			s.Guesses[key] = map[string]assist.Guess{}
		}
		for _, g := range guesses {
			s.Guesses[key][g.Key] = g
		}
	})
	return len(guesses), err
}
