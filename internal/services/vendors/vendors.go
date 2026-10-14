// Package vendors merges duplicate Invoice Ninja vendors as a workbench
// (services/workbench): "Nordhost" and "NORDHOST GmbH" split a vendor's
// history, so /vat proposes from half of it.
//
//	group (same first word) ─► pick the one to keep ─► review
//	  ─► every expense of the other moves over ─► the other is archived
package vendors

import (
	"context"
	"database/sql"
	"errors"
	"slices"

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
	// ErrNoNinja means the user reaches no Invoice Ninja.
	ErrNoNinja = errors.New("vendors.err_no_ninja")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("vendors.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja}}

// The action and field: act_<dup>=merge into_<dup>=<kept vendor>.
const (
	ActMerge  = "merge"
	FieldInto = "into"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldInto}

const (
	ProblemGone workbench.Problem = "vendors.problem_gone" // a vendor is gone or merged
	ProblemSelf workbench.Problem = "vendors.problem_self" // kept and merged are one
)

const prefKey = "vendors"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes []workbench.Scope
	Scope  string
	Choose bool
	Groups []metrics.VendorGroup
}

type book struct {
	scope  workbench.Scope
	choose bool
	conn   *model.Connection
	ninja  *sources.NinjaDataset
}

// Open lists the groups of duplicates.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Groups = metrics.VendorDuplicates(b.ninja)
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoNinja
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
	b.conn = b.scope.Conn(enums.ServiceInvoiceNinja)
	key, holder := sources.DataKey(enums.ServiceInvoiceNinja), model.UserHolder(who.UserID)
	res, err := svcdata.Get(ctx, d, key, nil, b.conn, holder, fresh)
	if err == nil && res.Pending {
		res, err = svcdata.Get(ctx, d, key, nil, b.conn, holder, svcdata.Cached)
	}
	if err != nil {
		return b, scopes, err
	}
	if b.ninja, _ = res.Data.(*sources.NinjaDataset); b.ninja == nil {
		return b, scopes, ErrNoNinja
	}
	return b, scopes, nil
}

// Item is a merge as the review shows it.
type Item struct {
	From, Into sources.NinjaVendor
	Expenses   int // in the dataset's window; Apply moves all
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
}

func (b book) vendor(key string) (sources.NinjaVendor, bool) {
	i := slices.IndexFunc(b.ninja.Vendors, func(v sources.NinjaVendor) bool { return v.Key == key })
	if i < 0 {
		return sources.NinjaVendor{}, false
	}
	return b.ninja.Vendors[i], true
}

// Check: both vendors are still there and differ.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	from, ok1 := b.vendor(s.Case)
	into, ok2 := b.vendor(s.Field(FieldInto))
	if !ok1 || !ok2 || s.Action != ActMerge {
		return it, ProblemGone
	}
	if from.Key == into.Key {
		return it, ProblemSelf
	}
	it.From, it.Into = from, into
	for _, e := range b.ninja.Expenses {
		if e.VendorKey == from.Key {
			it.Expenses++
		}
	}
	return it, workbench.ProblemNone
}

// Review checks the merges and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key}, err
}

// Apply merges against fresh data.
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
	holder := model.UserHolder(who.UserID)
	sctx, err := svcdata.SourceCtx(d, b.conn, holder)
	if err != nil {
		return workbench.Result{}, err
	}
	w := writer{book: b, sctx: sctx, to: outbound.Target{URL: b.conn.URL, Token: sctx.Secret, VerifyTLS: b.conn.VerifyTLS}}
	out := workbench.Apply[Item](ctx, d, who, w, plan, "vendors", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	sctx sources.Ctx
	to   outbound.Target
}

// Write moves every expense of the vendor, then archives it; a move
// refused stops before the archive, so nothing is left behind.
func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	keys, err := sources.NinjaVendorExpenses(ctx, w.sctx, p.Item.From.Key)
	if err != nil {
		return "", err
	}
	for _, k := range keys {
		if err := outbound.NinjaExpenseSet(ctx, w.to, k, map[string]string{"vendor_id": p.Item.Into.Key}); err != nil {
			return "", err
		}
	}
	return p.Item.Into.Name, outbound.NinjaVendorArchive(ctx, w.to, p.Item.From.Key)
}

func (w writer) Name(p workbench.Planned[Item]) string { return p.Item.From.Name }

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"from": p.Item.From.Name, "into": p.Item.Into.Name}
}
