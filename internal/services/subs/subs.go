// Package subs brings subscriptions in line with the bank as a workbench
// (services/workbench): Sure sees what is paid, Wallos lists the
// subscriptions, Invoice Ninja books the business ones as recurring
// expenses (metrics.SubCases).
//
//	Sure recurring ─┐
//	Wallos subs ────┼─► cases (add, new price, ended, twice) ─► review
//	Ninja recurring ┘     ─► written to Wallos or Invoice Ninja
package subs

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoSure means the user reaches no Sure.
	ErrNoSure = errors.New("subs.err_no_sure")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("subs.err_demo")
)

// need: Sure anchors; Wallos and Invoice Ninja are what the bench writes.
var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceSure},
	Optional: []enums.ServiceType{enums.ServiceWallos, enums.ServiceInvoiceNinja}}

// The fields: act_<case>=<metrics.SubAction> amount_<case>=44.90
// category_<case>=<Ninja category key> (a new recurring expense).
const (
	FieldAmount   = "amount"
	FieldCategory = "category"
	FieldTax      = "tax" // a rate or noTax, as on the VAT check
)

// Fields lists the fields, for the form.
var Fields = []string{FieldAmount, FieldCategory, FieldTax}

// noTax books a new recurring expense without tax.
const noTax = "none"

const (
	ProblemGone   workbench.Problem = "subs.problem_gone"   // settled meanwhile
	ProblemAmount workbench.Problem = "subs.problem_amount" // no amount above 0
)

const prefKey = "subs"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes     []workbench.Scope
	Scope      string
	Choose     bool
	Cases      []metrics.SubCase
	Currency   string
	Wallos     bool // the Verbund has a Wallos
	Ninja      bool // … an Invoice Ninja
	Categories []sources.NinjaCategory
	TaxRates   []sources.NinjaTaxRate
}

type book struct {
	scope  workbench.Scope
	choose bool
	today  time.Time
	conns  map[enums.ServiceType]*model.Connection
	bank   *sources.BankData
	wallos *sources.WallosDataset
	ninja  *sources.NinjaDataset
	cases  []metrics.SubCase
	num    enums.Notation // how the user types amounts
}

// Open lists the cases.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, today, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Cases, p.Currency = b.cases, b.bank.Currency
	p.Wallos, p.Ninja = b.wallos != nil, b.ninja != nil
	if b.ninja != nil {
		p.Categories, p.TaxRates = b.ninja.Categories, b.ninja.TaxRates
	}
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoSure
	}
	st, err := workbench.LoadState[state](d, who, prefKey)
	if err != nil {
		return book{}, scopes, err
	}
	if scope == "" {
		scope = st.Scope
	}

	b := book{today: metrics.Today(today), conns: map[enums.ServiceType]*model.Connection{}, num: i18n.NotationOf(who.Notation, who.Locale)}
	b.scope, b.choose = workbench.Pick(scopes, scope)
	holder := model.UserHolder(who.UserID)
	get := func(service enums.ServiceType) any {
		c := b.scope.Conn(service)
		if c == nil {
			return nil
		}
		key := sources.DataKey(service)
		res, err := svcdata.Get(ctx, d, key, nil, c, holder, fresh)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, key, nil, c, holder, svcdata.Cached)
		}
		if err != nil {
			return nil
		}
		b.conns[service] = c
		return res.Data
	}
	if b.bank, _ = get(enums.ServiceSure).(*sources.BankData); b.bank == nil {
		return b, scopes, ErrNoSure
	}
	b.wallos, _ = get(enums.ServiceWallos).(*sources.WallosDataset)
	b.ninja, _ = get(enums.ServiceInvoiceNinja).(*sources.NinjaDataset)

	in := metrics.SubInputs{Bank: b.bank, Wallos: b.wallos, Ninja: b.ninja, Today: b.today}
	in.MaxMonthly, in.Ignore, err = limits(d, b.conns[enums.ServiceSure])
	if err != nil {
		return b, scopes, err
	}
	if c := b.conns[enums.ServiceInvoiceNinja]; c != nil {
		in.Categories = sources.CategoriesOf(c.Options).Sure[b.conns[enums.ServiceSure].ID]
	}
	b.cases = metrics.SubCases(in)
	return b, scopes, nil
}

// limits are cross.wallos_missing's settings of the bank's space, so
// the bench leaves out what the hint does.
func limits(d *sql.DB, bank *model.Connection) (float64, []string, error) {
	var settings map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		sp, err := content.Space(tx, bank.SpaceID)
		if err == nil && sp != nil {
			settings = sp.Settings
		}
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	maxMonthly, ignore := rules.WallosLimits(settings)
	return maxMonthly, ignore, nil
}

// Item is a case as the review shows it.
type Item struct {
	metrics.SubCase
	Category string // a new recurring expense's, by name
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope    string
	Currency string
}

// Check: the case is still open with the same action; an add or new
// price has an amount.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.SubCase) bool { return c.Key == s.Case })
	if i < 0 || s.Action != string(b.cases[i].Action) {
		return it, ProblemGone
	}
	it.SubCase = b.cases[i]

	switch it.Action {
	case metrics.SubWallosAdd, metrics.SubWallosPrice, metrics.SubNinjaAdd, metrics.SubNinjaPrice:
		amount, err := i18n.ParseNum(s.Field(FieldAmount), b.num)
		if err != nil || amount <= 0 {
			return it, ProblemAmount
		}
		it.Amount = amount
	}
	if it.Action != metrics.SubNinjaAdd {
		return it, workbench.ProblemNone
	}

	if key := s.Field(FieldCategory); key != "" {
		it.CategoryKey = key
	}
	if j := slices.IndexFunc(b.ninja.Categories, func(c sources.NinjaCategory) bool { return c.Key == it.CategoryKey }); j >= 0 {
		it.Category = b.ninja.Categories[j].Name
	}

	// The tax as chosen; without a field the vendor's last expense's.
	switch tax := s.Field(FieldTax); {
	case tax == noTax:
		it.TaxName, it.TaxRate = "", 0
	case tax != "":
		rate, err := strconv.ParseFloat(tax, 64)
		j := slices.IndexFunc(b.ninja.TaxRates, func(r sources.NinjaTaxRate) bool { return r.Rate == rate })
		switch {
		case err != nil:
		case it.TaxName != "" && it.TaxRate == rate:
			// The vendor's own rate, offered even when Invoice Ninja no
			// longer has it set up: kept as it is.
		case j >= 0:
			it.TaxName, it.TaxRate = b.ninja.TaxRates[j].Name, rate
		}
	}
	return it, workbench.ProblemNone
}

// Review checks the steps and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key, Currency: b.bank.Currency}, err
}

// Apply writes the confirmed steps against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Force)
	if err != nil {
		return workbench.Result{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	if err != nil {
		return workbench.Result{}, err
	}

	// The services the plan writes to: each writable, none a demo.
	w := writer{book: b, to: map[enums.ServiceType]outbound.Target{}}
	for _, p := range plan.Steps {
		service := serviceOf(p.Item.Action)
		if _, ok := w.to[service]; ok || p.Problem != workbench.ProblemNone {
			continue
		}
		c := b.conns[service]
		if err := connections.Writable(d, who, c.ID); err != nil {
			return workbench.Result{}, err
		}
		if sources.IsDemo(c.URL) {
			return workbench.Result{}, ErrDemo
		}
		token, err := svcdata.Secret(ctx, d, c, model.UserHolder(who.UserID))
		if err != nil {
			return workbench.Result{}, err
		}
		w.to[service] = outbound.Target{URL: c.URL, Token: token, VerifyTLS: c.VerifyTLS}
	}

	out := workbench.Apply[Item](ctx, d, who, w, plan, "subs", ip)
	for service := range w.to {
		svcdata.Forget(b.conns[service].ID)
	}
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

// serviceOf is the service an action writes to.
func serviceOf(a metrics.SubAction) enums.ServiceType {
	if strings.HasPrefix(string(a), "ninja_") {
		return enums.ServiceInvoiceNinja
	}
	return enums.ServiceWallos
}

type writer struct {
	book
	to map[enums.ServiceType]outbound.Target
}

// Write changes one entry in Wallos or Invoice Ninja; an ended or
// doubled one is stopped or removed, not edited.
func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	it := p.Item
	to := w.to[serviceOf(it.Action)]
	switch it.Action {
	case metrics.SubWallosAdd:
		cycle, frequency := sources.WallosCycle(it.Months)
		_, err := outbound.WallosAdd(ctx, to, outbound.WallosNew{Name: it.Name, Price: it.Amount, CurrencyID: w.wallos.Main,
			Cycle: cycle, Frequency: frequency, Next: it.Next})
		return it.Name, err
	case metrics.SubWallosPrice:
		return it.Name, outbound.WallosPrice(ctx, to, it.Wallos.ID, it.Amount)
	case metrics.SubWallosEnd:
		return it.Name, outbound.WallosEnd(ctx, to, it.Wallos.ID, w.today.Format(time.DateOnly))
	case metrics.SubWallosMerge:
		return it.Name, outbound.WallosDelete(ctx, to, it.Wallos.ID)
	case metrics.SubNinjaAdd:
		return w.ninjaAdd(ctx, to, it)
	case metrics.SubNinjaPrice:
		return it.Name, outbound.NinjaRecurringAmount(ctx, to, it.Ninja.Key, it.Amount)
	case metrics.SubNinjaEnd:
		return it.Name, outbound.NinjaRecurringBulk(ctx, to, it.Ninja.Key, outbound.RecurringStop)
	case metrics.SubNinjaMerge:
		return it.Name, outbound.NinjaRecurringBulk(ctx, to, it.Ninja.Key, outbound.RecurringArchive)
	}
	return "", errors.New(string(ProblemGone))
}

// ninjaAdd creates the vendor if Invoice Ninja has none, then the
// recurring expense.
func (w writer) ninjaAdd(ctx context.Context, to outbound.Target, it Item) (string, error) {
	frequency, ok := sources.NinjaFrequency(it.Months)
	if !ok {
		return "", errors.New(string(ProblemGone))
	}
	vendor := it.VendorKey
	if vendor == "" {
		key, err := outbound.NinjaVendorCreate(ctx, to, it.Vendor)
		if err != nil {
			return "", err
		}
		vendor = key
	}
	_, number, err := outbound.NinjaRecurringCreate(ctx, to, outbound.NinjaRecurringNew{VendorKey: vendor, CategoryKey: it.CategoryKey,
		Notes: it.Name, Amount: it.Amount, Frequency: frequency, Next: it.Next, TaxName: it.TaxName, TaxRate: it.TaxRate})
	return strings.TrimSpace(it.Vendor + " " + number), err
}

func (w writer) Name(p workbench.Planned[Item]) string { return p.Item.Name }

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	out := map[string]any{"name": p.Item.Name}
	if p.Item.Amount > 0 {
		out["amount"] = strconv.FormatFloat(p.Item.Amount, 'f', 2, 64)
	}
	if p.Item.Old > 0 {
		out["old"] = strconv.FormatFloat(p.Item.Old, 'f', 2, 64)
	}
	return out
}
