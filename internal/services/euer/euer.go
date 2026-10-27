// Package euer checks and completes the EÜR line of Invoice Ninja
// expenses, a custom field, as a workbench (services/workbench): each
// expense of the year whose field is empty is proposed the line of its
// vendor's last expense; the user ticks, reviews, and Andon writes.
// The line belongs to the expense, not its category (one category goes
// to several lines). The VAT check (/vat) fills it on creating already.
//
//	expense, field empty ── vendor's last line ──► tick ──► review ──► Ninja
//	                      └─ none: the user picks one
package euer

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoNinja means the user reaches no Invoice Ninja.
	ErrNoNinja = errors.New("euer.err_no_ninja")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("euer.err_demo")
)

// need is the bench's service.
var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja},
	Optional: []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly}}

// The bench's action and field: act_Kx9=set line_Kx9=Arbeitsmittel.
const (
	ActSet    = "set"
	FieldLine = "line"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldLine}

const (
	ProblemGone  workbench.Problem = "euer.problem_gone"  // filled or gone meanwhile
	ProblemLine  workbench.Problem = "euer.problem_line"  // no line picked
	ProblemValue workbench.Problem = "euer.problem_value" // not one of the dropdown's
)

// yearsShown is how many years the page offers, this one included.
const yearsShown = 3

// prefKey keeps the Verbund last used.
const prefKey = "euer"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund and year.
type Page struct {
	Scopes    []workbench.Scope
	Scope     string
	Choose    bool
	Year      int
	Years     []int
	Slot      int    // the EÜR field, 0 = not chosen (Verbund → Kategorien)
	Label     string // its name
	Lines     []string
	Dropdown  bool // the field takes only Lines
	Cases     []metrics.EuerCase
	Suggested int // cases with a proposed line
	SpaceID   int64
	LinkID    int64 // the Verbund, for its categories page; 0 none
	Ninja     int64 // its Invoice Ninja and Sure or Firefly (0 none), for
	Bank      int64 // the categories page outside a Verbund
}

// book is the loaded data.
type book struct {
	scope  workbench.Scope
	choose bool
	conn   *model.Connection
	ninja  *sources.NinjaDataset
	cats   sources.Categories
	year   int
}

// Open builds the page.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, year int, today time.Time) (Page, error) {
	p := Page{Year: year}
	if p.Year < today.Year()-yearsShown+1 || p.Year > today.Year() {
		p.Year = today.Year()
	}
	for y := today.Year(); y > today.Year()-yearsShown; y-- {
		p.Years = append(p.Years, y)
	}
	b, scopes, err := load(ctx, d, who, scope, p.Year, svcdata.Stored)
	p.Scopes, p.Scope, p.Choose = scopes, b.scope.Key, b.choose
	if err != nil {
		return p, err
	}
	p.SpaceID, p.LinkID, p.Slot, p.Ninja = b.conn.SpaceID, b.scope.Link, b.cats.EuerSlot, b.conn.ID
	for _, s := range need.Optional {
		if c := b.scope.Conn(s); c != nil && p.Bank == 0 {
			p.Bank = c.ID
		}
	}
	if p.Slot == 0 {
		return p, nil
	}
	field := b.ninja.ExpenseFields[p.Slot-1]
	p.Label, p.Lines, p.Dropdown = field.Label, metrics.EuerChoices(b.ninja, p.Slot), len(field.Options) > 0
	p.Cases = metrics.EuerCases(b.ninja, p.Slot, strconv.Itoa(p.Year)+"-01-01")
	p.Cases = slices.DeleteFunc(p.Cases, func(c metrics.EuerCase) bool { return !strings.HasPrefix(c.Expense.Date, strconv.Itoa(p.Year)) })
	for _, c := range p.Cases {
		if c.Line != "" {
			p.Suggested++
		}
	}
	return p, nil
}

// load picks the Verbund and reads its Invoice Ninja.
func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, year int, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
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
	b := book{year: year}
	b.scope, b.choose = workbench.Pick(scopes, scope)
	b.conn = b.scope.Conn(enums.ServiceInvoiceNinja)
	b.cats = sources.CategoriesOf(b.conn.Options)

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

// Item is what a step resolves to.
type Item struct {
	Expense sources.NinjaExpense
	Line    string
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
	Year  int
	Label string
}

// Check: the expense must still have an empty field and a line be
// given, one of the dropdown's if the field is one.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.ninja.Expenses, func(e sources.NinjaExpense) bool { return e.Key == s.Case })
	if i < 0 || b.cats.EuerSlot == 0 || b.ninja.Expenses[i].Custom[b.cats.EuerSlot-1] != "" {
		return it, ProblemGone
	}
	it.Expense, it.Line = b.ninja.Expenses[i], strings.TrimSpace(s.Field(FieldLine))
	if it.Line == "" || s.Action != ActSet {
		return it, ProblemLine
	}
	if opts := b.ninja.ExpenseFields[b.cats.EuerSlot-1].Options; len(opts) > 0 && !slices.Contains(opts, it.Line) {
		return it, ProblemValue
	}
	return it, workbench.ProblemNone
}

// Review checks the steps and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, year int, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, year, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	out := Plan{Plan: plan, Scope: b.scope.Key, Year: year}
	if b.cats.EuerSlot > 0 {
		out.Label = b.ninja.ExpenseFields[b.cats.EuerSlot-1].Label
	}
	return out, err
}

// Apply writes the confirmed steps against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, year int, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, year, svcdata.Force)
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
	out := workbench.Apply[Item](ctx, d, who, w, plan, "euer", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

// Unset empties an expense's EÜR field again (the journal's undo).
func Unset(ctx context.Context, d *sql.DB, who *access.Principal, scope, expense string, slot int, ip string) error {
	b, _, err := load(ctx, d, who, scope, time.Now().Year(), svcdata.Cached)
	if err != nil {
		return err
	}
	if err := connections.Writable(d, who, b.conn.ID); err != nil {
		return err
	}
	if sources.IsDemo(b.conn.URL) {
		return ErrDemo
	}
	token, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
	if err != nil {
		return err
	}
	to := outbound.Target{URL: b.conn.URL, Token: token, VerifyTLS: b.conn.VerifyTLS}
	if err := outbound.NinjaExpenseSet(ctx, to, expense, map[string]string{customValue(slot): ""}); err != nil {
		return err
	}
	svcdata.Forget(b.conn.ID)
	return auditsvc.Log(d, &who.UserID, "euer.unset", expense, ip, map[string]any{"slot": strconv.Itoa(slot)})
}

func customValue(slot int) string { return "custom_value" + strconv.Itoa(slot) }

// writer sets the field in one Invoice Ninja.
type writer struct {
	book
	to outbound.Target
}

func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	return p.Item.Expense.Number, outbound.NinjaExpenseSet(ctx, w.to, p.Item.Expense.Key, map[string]string{customValue(w.cats.EuerSlot): p.Item.Line})
}

func (w writer) Name(p workbench.Planned[Item]) string {
	return strings.TrimSpace(p.Item.Expense.Number + " " + p.Item.Expense.Notes)
}

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"expense": p.Item.Expense.Key, "line": p.Item.Line, "slot": strconv.Itoa(w.cats.EuerSlot), "scope": w.scope.Key}
}
