package vatcheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/receipts"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// Field names of a step (workbench.Step.Fields), as the form sends
// them: exp_Kx9=…; tax is a rate or "none".
const (
	FieldExpense  = "exp"
	FieldInvoice  = "inv"
	FieldPayment  = "pay"
	FieldVendor   = "vendor"
	FieldCategory = "cat" // a key
	FieldTax      = "tax"
	FieldScan     = "scan" // create: the Paperless scan to link
)

// Fields lists them, for the form.
var Fields = []string{FieldExpense, FieldInvoice, FieldPayment, FieldVendor, FieldCategory, FieldTax, FieldScan}

// noTax is the tax field of an expense without tax.
const noTax = "none"

const (
	ProblemGone     workbench.Problem = "vatcheck.problem_gone"      // booked meanwhile
	ProblemTarget   workbench.Problem = "vatcheck.problem_target"    // expense or invoice no longer fits
	ProblemNoVendor workbench.Problem = "vatcheck.problem_no_vendor" // a new expense needs a vendor
)

// ErrGone means the transaction is not (or no longer) archived.
var ErrGone = errors.New("vatcheck.problem_gone")

// Item is what a step resolves to, as the review shows it.
type Item struct {
	Txn       sources.NinjaBankTxn
	Expense   *sources.NinjaExpense
	Invoice   *sources.NinjaInvoice
	Payment   *sources.NinjaPayment
	Vendor    string
	NewVendor bool   // the vendor is created first
	Category  string // its name
	TaxName   string
	TaxRate   float64
	Taxed     bool
	Tax       float64 // input VAT in the amount
	Scan      int64
	ScanTitle string // the scan linked to a new expense
	Euer      string // the EÜR line the new expense gets (Verbund → Kategorien)
}

// Planned is a step as it would be written.
type Planned = workbench.Planned[Item]

// Plan is the review: what Apply would write.
type Plan struct {
	workbench.Plan[Item]
	Scope    string
	Currency string
}

// Review checks the steps against the current data and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	for i := range plan.Steps {
		scanTitle(ctx, d, who, b.conn.ID, &plan.Steps[i])
	}
	return Plan{Plan: plan, Scope: b.scope.Key, Currency: b.ninja.Currency}, err
}

// scanTitle names the scan a new expense gets; one no longer free is
// dropped (receipts.ScansFor offers only free ones).
func scanTitle(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, p *Planned) {
	if p.Item.Scan == 0 || p.Action != string(metrics.ActionCreate) || p.Problem != workbench.ProblemNone {
		return
	}
	scans, _ := receipts.ScansFor(ctx, d, who, connID, p.Item.Txn.Amount, p.Item.Txn.Date, p.Item.Vendor)
	for _, c := range scans {
		if c.Doc.ID == p.Item.Scan {
			p.Item.ScanTitle = c.Doc.Title
			return
		}
	}
	p.Item.Scan = 0
	delete(p.Fields, FieldScan)
}

// Check resolves one step; the transaction must still be open and its
// target must still fit.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := indexOf(b.ninja.BankTxns, func(t sources.NinjaBankTxn) bool { return t.Key == s.Case })
	if i < 0 || b.ninja.BankTxns[i].Status == sources.BankDone {
		return it, ProblemGone
	}
	it.Txn = b.ninja.BankTxns[i]
	taken := metrics.LinkedExpenses(b.ninja)
	switch metrics.BankAction(s.Action) {
	case metrics.ActionLink:
		j := indexOf(b.ninja.Expenses, func(e sources.NinjaExpense) bool { return e.Key == s.Field(FieldExpense) })
		if j < 0 || taken[s.Field(FieldExpense)] || math.Abs(b.ninja.Expenses[j].Amount-it.Txn.Amount) > amountTol {
			return it, ProblemTarget
		}
		it.Expense = &b.ninja.Expenses[j]
	case metrics.ActionInvoice:
		// Ninja pays at most the balance: a part payment fits too.
		j := indexOf(b.ninja.Invoices, func(inv sources.NinjaInvoice) bool { return inv.Key == s.Field(FieldInvoice) })
		if j < 0 || it.Txn.Debit || b.ninja.Invoices[j].Balance <= 0 || it.Txn.Amount > b.ninja.Invoices[j].Balance+amountTol {
			return it, ProblemTarget
		}
		it.Invoice = &b.ninja.Invoices[j]
	case metrics.ActionPayment:
		j := indexOf(b.ninja.Payments, func(pay sources.NinjaPayment) bool { return pay.Key == s.Field(FieldPayment) })
		if j < 0 || it.Txn.Debit || metrics.LinkedPayments(b.ninja)[s.Field(FieldPayment)] || math.Abs(b.ninja.Payments[j].Amount-it.Txn.Amount) > amountTol {
			return it, ProblemTarget
		}
		it.Payment = &b.ninja.Payments[j]
	case metrics.ActionCreate:
		return b.created(s, it)
	case metrics.ActionSkip:
	default:
		return it, ProblemTarget
	}
	return it, workbench.ProblemNone
}

// created resolves a new expense: vendor, category and tax.
func (b book) created(s *workbench.Step, it Item) (Item, workbench.Problem) {
	it.Vendor = strings.TrimSpace(s.Field(FieldVendor))
	if it.Vendor == "" || !it.Txn.Debit {
		return it, ProblemNoVendor
	}
	it.NewVendor = b.vendorKey(it.Vendor) == ""
	it.Category = categoryName(b.ninja, s.Field(FieldCategory))
	if b.cats.EuerSlot > 0 {
		it.Euer = b.cats.Euer[s.Field(FieldCategory)]
	}
	if rate, err := strconv.ParseFloat(s.Field(FieldTax), 64); err == nil {
		it.TaxRate, it.Taxed = rate, true
		it.TaxName = b.taxName(rate)
		it.Tax = math.Round(it.Txn.Amount*rate/(100+rate)*cents) / cents
	} else {
		s.Fields[FieldTax] = noTax
	}
	it.Scan, _ = strconv.ParseInt(s.Field(FieldScan), 10, 64)
	return it, workbench.ProblemNone
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

// Apply writes the confirmed steps against fresh data; each is checked
// again, so a transaction booked meanwhile is not booked twice.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Force)
	if err != nil {
		return workbench.Result{}, err
	}
	if err := connections.Writable(d, who, b.conn.ID); err != nil {
		return workbench.Result{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	if err != nil {
		return workbench.Result{}, err
	}
	if plan.Writes() > 0 && sources.IsDemo(b.conn.URL) {
		return workbench.Result{}, ErrDemo
	}
	token, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
	if err != nil {
		return workbench.Result{}, err
	}

	w := &writer{to: outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}, book: b, d: d, who: who, ip: ip}
	out := workbench.Apply[Item](ctx, d, who, w, plan, auditPrefix, ip)
	svcdata.Forget(b.conn.ID)
	err = saveState(d, who, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

// auditPrefix starts the audit entries of written steps: vatcheck.link.
const auditPrefix = "vatcheck"

// writer books steps in one Invoice Ninja.
type writer struct {
	book
	to  outbound.Target
	d   *sql.DB
	who *access.Principal
	ip  string
}

func (w *writer) Name(p Planned) string { return p.Item.Txn.Payer }

func (w *writer) Audit(p Planned) map[string]any {
	return map[string]any{"txn": p.Item.Txn.Key, "amount": fmt.Sprintf("%.2f", p.Item.Txn.Amount), "scope": w.scope.Key}
}

// Reason is a refusal's text: a known one's key, else the message.
func (w *writer) Reason(err error) string {
	if errors.Is(err, outbound.ErrNotBooked) {
		return "vatcheck.problem_not_booked"
	}
	if errors.Is(err, outbound.ErrNotArchived) {
		return "vatcheck.problem_not_archived"
	}
	return err.Error()
}

// Write books one step and returns what it booked against (an expense
// or invoice number, the payee of an archived one).
func (w *writer) Write(ctx context.Context, p Planned) (string, error) {
	it := p.Item
	switch metrics.BankAction(p.Action) {
	case metrics.ActionSkip:
		return it.Txn.Payer, outbound.NinjaBankArchive(ctx, w.to, it.Txn.Key, outbound.BankArchiveOn)
	case metrics.ActionLink:
		return it.Expense.Number, outbound.NinjaBankLink(ctx, w.to, it.Txn.Key, outbound.NinjaBankTarget{Expense: it.Expense.Key})
	case metrics.ActionInvoice:
		return it.Invoice.Number, outbound.NinjaBankLink(ctx, w.to, it.Txn.Key, outbound.NinjaBankTarget{Invoices: []string{it.Invoice.Key}})
	case metrics.ActionPayment:
		return it.Payment.Number, outbound.NinjaBankLink(ctx, w.to, it.Txn.Key, outbound.NinjaBankTarget{Payment: it.Payment.Key})
	}

	// A new expense: vendor first if new, the expense with its tax, then
	// the link that marks the transaction booked.
	vendor := w.vendorKey(it.Vendor)
	if vendor == "" {
		key, err := outbound.NinjaVendorCreate(ctx, w.to, it.Vendor)
		if err != nil {
			return "", err
		}
		vendor = key
	}
	body := map[string]any{"amount": it.Txn.Amount, "date": it.Txn.Date, "payment_date": it.Txn.Date, "vendor_id": vendor,
		"public_notes": it.Txn.Description, "transaction_reference": it.Txn.Description, "uses_inclusive_taxes": true}
	if cat := p.Field(FieldCategory); cat != "" {
		body["category_id"] = cat
	}
	if it.Taxed {
		body["tax_name1"], body["tax_rate1"] = it.TaxName, it.TaxRate
	}
	if it.Euer != "" {
		body["custom_value"+strconv.Itoa(w.cats.EuerSlot)] = it.Euer
	}
	key, number, err := outbound.NinjaExpenseCreate(ctx, w.to, body)
	if err != nil {
		return "", err
	}
	if err := outbound.NinjaBankLink(ctx, w.to, it.Txn.Key, outbound.NinjaBankTarget{Expense: key}); err != nil {
		return number, fmt.Errorf("%s: %w", number, err)
	}

	// The new expense gets its scan, both sides (receipts.Link).
	if it.Scan == 0 {
		return number, nil
	}
	if _, err := receipts.Link(ctx, w.d, w.who, key, []int64{it.Scan}, w.ip); err != nil {
		return number, workbench.Partly{Target: number, Reason: "vatcheck.problem_scan"}
	}
	return number, nil
}

// Restore brings a transaction archived as private back into the feed.
func Restore(ctx context.Context, d *sql.DB, who *access.Principal, scope, txn string, ip string) error {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
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
func SaveRoles(ctx context.Context, d *sql.DB, who *access.Principal, scope string, roles []Role, ip string) error {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
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
func Guess(ctx context.Context, d *sql.DB, who *access.Principal, scope, spanKey string, today time.Time) (int, error) {
	page, err := Open(ctx, d, who, scope, spanKey, today)
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
		s.Scope = page.Scope
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
