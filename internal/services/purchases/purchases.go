// Package purchases makes recent purchases complete as a workbench
// (services/workbench): an asset of the last year needs its cost and
// warranty in Snipe-IT and an expense with receipt in Invoice Ninja
// (metrics.Purchases). Andon writes the warranty; for a missing expense
// or receipt the page leads to the VAT check and the receipt hunt.
//
//	Snipe-IT ─ assets ──────┐
//	Invoice Ninja ─ expenses ┴─► gaps ─► warranty in Snipe-IT │ links
package purchases

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

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
	// ErrNoSnipe means the user reaches no Snipe-IT.
	ErrNoSnipe = errors.New("purchases.err_no_snipe")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("purchases.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceSnipeIT},
	Optional: []enums.ServiceType{enums.ServiceInvoiceNinja}}

// The action and field: act_<asset>=warranty months_<asset>=24.
const (
	ActWarranty = "warranty"
	FieldMonths = "months"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldMonths}

// DefaultMonths is the warranty proposed: the statutory two years.
const DefaultMonths = 24

// maxMonths bounds a warranty typed in.
const maxMonths = 120

const (
	ProblemGone   workbench.Problem = "purchases.problem_gone"   // warranty set meanwhile
	ProblemMonths workbench.Problem = "purchases.problem_months" // no sensible months
)

// optLinkSlot is the Invoice Ninja option naming the expense field that
// holds the receipt link (services/receipts).
const optLinkSlot = "receipt_link_slot"

const prefKey = "purchases"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes []workbench.Scope
	Scope  string
	Choose bool
	Cases  []metrics.PurchaseCase
	Ninja  bool // expenses checked
	Linked bool // receipts checked (the link field is known)
	Months int
}

type book struct {
	scope  workbench.Scope
	choose bool
	conn   *model.Connection
	cases  []metrics.PurchaseCase
	ninja  bool
	linked bool
}

// Open lists the purchases with gaps.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, today, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose, Months: DefaultMonths}
	if err != nil {
		return p, err
	}
	p.Cases, p.Ninja, p.Linked = b.cases, b.ninja, b.linked
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoSnipe
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
	b.conn = b.scope.Conn(enums.ServiceSnipeIT)

	holder := model.UserHolder(who.UserID)
	get := func(c *model.Connection, service enums.ServiceType, f svcdata.Freshness) any {
		if c == nil {
			return nil
		}
		res, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, f)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Cached)
		}
		if err != nil {
			return nil
		}
		return res.Data
	}
	snipe, _ := get(b.conn, enums.ServiceSnipeIT, fresh).(*sources.SnipeDataset)
	if snipe == nil {
		return b, scopes, ErrNoSnipe
	}
	nc := b.scope.Conn(enums.ServiceInvoiceNinja)
	ninja, _ := get(nc, enums.ServiceInvoiceNinja, svcdata.Stored).(*sources.NinjaDataset)
	slot := 0
	if nc != nil {
		slot, _ = nc.Options[optLinkSlot].(int)
		if f, ok := nc.Options[optLinkSlot].(float64); ok {
			slot = int(f)
		}
	}
	b.ninja, b.linked = ninja != nil, ninja != nil && slot > 0
	b.cases = metrics.Purchases(snipe, ninja, slot, today)
	return b, scopes, nil
}

// Item is a warranty as the review shows it.
type Item struct {
	metrics.PurchaseCase
	Months int
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
}

// Check: the asset still lacks its warranty, months are sensible.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.PurchaseCase) bool { return c.Key == s.Case })
	if i < 0 || s.Action != ActWarranty || !b.cases[i].Has(metrics.GapWarranty) {
		return it, ProblemGone
	}
	it.PurchaseCase = b.cases[i]
	it.Months, _ = strconv.Atoi(s.Field(FieldMonths))
	if it.Months <= 0 || it.Months > maxMonths {
		return it, ProblemMonths
	}
	return it, workbench.ProblemNone
}

// Review checks the warranties and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key}, err
}

// Apply writes the warranties against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Force)
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
	out := workbench.Apply[Item](ctx, d, who, w, plan, "purchases", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to outbound.Target
}

func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	return p.Item.Asset.Name, outbound.SnipeWarranty(ctx, w.to, p.Item.Asset.ID, p.Item.Months)
}

func (w writer) Name(p workbench.Planned[Item]) string { return p.Item.Asset.Name }

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"asset": p.Item.Asset.Name, "months": strconv.Itoa(p.Item.Months)}
}
