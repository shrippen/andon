// Package categorize files Sure's bookings without category as a
// workbench (services/workbench), one merchant at a time
// (metrics.Uncategorized):
//
//	merchant's bookings without category ─► category proposed from its
//	filed ones ─► review ─► each booking filed in Sure (fresh data)
package categorize

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoSure means the user reaches no Sure.
	ErrNoSure = errors.New("categorize.err_no_sure")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("categorize.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceSure}}

// The action and field: act_<merchant>=file category_<merchant>=<id>.
const (
	ActFile       = "file"
	FieldCategory = "category"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldCategory}

const (
	ProblemGone     workbench.Problem = "categorize.problem_gone"     // filed meanwhile
	ProblemCategory workbench.Problem = "categorize.problem_category" // none chosen, or gone
)

const prefKey = "categorize"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes     []workbench.Scope
	Scope      string
	Choose     bool
	Cases      []metrics.CatCase
	Categories []sources.BankCategory
	Currency   string
}

type book struct {
	scope  workbench.Scope
	choose bool
	conn   *model.Connection
	bank   *sources.BankData
	cases  []metrics.CatCase
}

// Open lists the merchants with bookings to file.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Cases, p.Categories, p.Currency = b.cases, b.bank.Categories, b.bank.Currency
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
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
	var b book
	b.scope, b.choose = workbench.Pick(scopes, scope)
	b.conn = b.scope.Conn(enums.ServiceSure)
	key, holder := sources.DataKey(enums.ServiceSure), model.UserHolder(who.UserID)
	res, err := svcdata.Get(ctx, d, key, nil, b.conn, holder, fresh)
	if err == nil && res.Pending {
		res, err = svcdata.Get(ctx, d, key, nil, b.conn, holder, svcdata.Cached)
	}
	if err != nil {
		return b, scopes, err
	}
	if b.bank, _ = res.Data.(*sources.BankData); b.bank == nil {
		return b, scopes, ErrNoSure
	}
	b.cases = metrics.Uncategorized(b.bank)
	return b, scopes, nil
}

// Item is a merchant's filing as the review shows it.
type Item struct {
	metrics.CatCase
	Category string // by name
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope    string
	Currency string
}

// Check: the merchant still has bookings without category, and the
// chosen category exists.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.CatCase) bool { return c.Key == s.Case })
	if i < 0 || s.Action != ActFile {
		return it, ProblemGone
	}
	it.CatCase = b.cases[i]
	it.CategoryID = s.Field(FieldCategory)
	j := slices.IndexFunc(b.bank.Categories, func(c sources.BankCategory) bool { return c.ID == it.CategoryID })
	if j < 0 {
		return it, ProblemCategory
	}
	it.Category = b.bank.Categories[j].Name
	return it, workbench.ProblemNone
}

// Review checks the filings and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key, Currency: b.bank.Currency}, err
}

// Apply files the bookings against fresh data: only those still
// without category.
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
	w := writer{book: b, to: outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}}
	out := workbench.Apply[Item](ctx, d, who, w, plan, "categorize", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to outbound.Target
}

// Write files each booking; one refused after others were filed is
// done in part.
func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	for n, t := range p.Item.Txns {
		err := outbound.SureCategorySet(ctx, w.to, t.ID, p.Item.CategoryID)
		if err == nil {
			continue
		}
		if n == 0 {
			return "", err
		}
		return p.Item.Merchant, workbench.Partly{Target: p.Item.Merchant, Reason: err.Error()}
	}
	return p.Item.Merchant, nil
}

func (w writer) Name(p workbench.Planned[Item]) string { return p.Item.Merchant }

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"merchant": p.Item.Merchant, "category": p.Item.Category, "bookings": strconv.Itoa(len(p.Item.Txns))}
}
