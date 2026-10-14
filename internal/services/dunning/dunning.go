// Package dunning sends payment reminders as a workbench
// (services/workbench) on Invoice Ninja's own schedule (company settings
// → reminders, metrics.DunningCases): invoices whose next reminder is
// due and not sent, a text by Claude on request, sent by Invoice Ninja's
// mail and template after review. Mails go to clients: nothing is ticked
// by itself.
//
//	overdue invoices ─► next stage ─► (Claude: subject, body) ─► review ─► Ninja mails
package dunning

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
	"andon/internal/services/assist"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoNinja means the user reaches no Invoice Ninja.
	ErrNoNinja = errors.New("dunning.err_no_ninja")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("dunning.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja}}

// The action and fields: act_<invoice>=remind stage_… subject_… body_….
const (
	ActRemind    = "remind"
	FieldStage   = "stage"
	FieldSubject = "subject"
	FieldBody    = "body"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldStage, FieldSubject, FieldBody}

const (
	ProblemGone  workbench.Problem = "dunning.problem_gone"  // paid or reminded meanwhile
	ProblemStage workbench.Problem = "dunning.problem_stage" // another reminder is due now
)

// lateDays: a client paying later than this usually pays late.
const lateDays = 30

// drafted caps one request for texts.
const drafted = 20

const prefKey = "dunning"

type state struct {
	Scope  string                         `json:"scope,omitempty"`
	Drafts map[string]assist.ReminderText `json:"drafts,omitempty"` // per invoice key and stage: "Kx9/2"
}

// Case is an invoice due for a reminder, with a drafted text if asked.
type Case struct {
	metrics.DunningCase
	Text *assist.ReminderText
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes   []workbench.Scope
	Scope    string
	Choose   bool
	Cases    []Case
	Currency string
	URL      string // Invoice Ninja, for links
	Assist   bool
	Plan     metrics.DunningPlan // Invoice Ninja's reminder schedule, or the defaults
}

type book struct {
	scope  workbench.Scope
	choose bool
	conn   *model.Connection
	ninja  *sources.NinjaDataset
	state  state
	cases  []metrics.DunningCase
}

func draftKey(invoice string, stage int) string { return invoice + "/" + strconv.Itoa(stage) }

// Open lists the invoices due for a reminder.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, today, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose, Assist: assist.Enabled()}
	if err != nil {
		return p, err
	}
	p.Currency, p.URL, p.Plan = b.ninja.Currency, strings.TrimRight(b.conn.URL, "/"), metrics.DunningPlanOf(b.ninja)
	for _, c := range b.cases {
		cs := Case{DunningCase: c}
		if t, ok := b.state.Drafts[draftKey(c.Key, c.Next)]; ok {
			cs.Text = &t
		}
		p.Cases = append(p.Cases, cs)
	}
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoNinja
	}
	var b book
	if b.state, err = workbench.LoadState[state](d, who, prefKey); err != nil {
		return b, scopes, err
	}
	if scope == "" {
		scope = b.state.Scope
	}
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
	b.cases = metrics.DunningCases(b.ninja, metrics.Today(today))
	return b, scopes, nil
}

// Draft asks Claude for the texts of the cases still without one; it
// returns how many it wrote.
func Draft(ctx context.Context, d *sql.DB, who *access.Principal, scope string, today time.Time) (int, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Cached)
	if err != nil {
		return 0, err
	}
	// A client paying in more than the invoice term plus the gap pays late.
	late := map[int64]bool{}
	for client, days := range metrics.NinjaPaymentDays(b.ninja, metrics.CenterMedian) {
		late[client] = days > lateDays
	}
	texts := map[string]assist.ReminderText{}
	for _, c := range b.cases {
		key := draftKey(c.Key, c.Next)
		if _, done := b.state.Drafts[key]; done || len(texts) >= drafted {
			continue
		}
		t, err := assist.DraftReminder(ctx, who.Locale, assist.Reminder{Client: c.Client, Number: c.Number, Amount: c.Amount,
			Balance: c.Balance, Currency: b.ninja.Currency, Due: c.DueDate, OverdueDays: c.OverdueDays, Stage: c.Next, PaysLate: late[c.ClientID]})
		if err != nil {
			return len(texts), err
		}
		texts[key] = t
	}
	err = workbench.SaveState(d, who, prefKey, func(s *state) {
		s.Scope = b.scope.Key
		if s.Drafts == nil {
			s.Drafts = map[string]assist.ReminderText{}
		}
		for k, t := range texts {
			s.Drafts[k] = t
		}
	})
	return len(texts), err
}

// Item is a reminder as the review shows it.
type Item struct {
	Case          metrics.DunningCase
	Subject, Body string
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope    string
	Currency string
}

// Check: the invoice is still due for exactly that reminder.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.DunningCase) bool { return c.Key == s.Case })
	if i < 0 || s.Action != ActRemind {
		return it, ProblemGone
	}
	it.Case = b.cases[i]
	it.Subject, it.Body = strings.TrimSpace(s.Field(FieldSubject)), strings.TrimSpace(s.Field(FieldBody))
	if stage, _ := strconv.Atoi(s.Field(FieldStage)); stage != it.Case.Next {
		return it, ProblemStage
	}
	return it, workbench.ProblemNone
}

// Review checks the reminders and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step, today time.Time) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, today, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key, Currency: b.ninja.Currency}, err
}

// Apply sends the confirmed reminders against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step, today time.Time, ip string) (workbench.Result, error) {
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
	out := workbench.Apply[Item](ctx, d, who, w, plan, "dunning", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to outbound.Target
}

func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	c := p.Item.Case
	return c.Number, outbound.NinjaReminder(ctx, w.to, c.Key, c.Next, p.Item.Subject, p.Item.Body)
}

func (w writer) Name(p workbench.Planned[Item]) string {
	return strings.TrimSpace(p.Item.Case.Number + " " + p.Item.Case.Client)
}

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"client": p.Item.Case.Client, "stage": strconv.Itoa(p.Item.Case.Next)}
}
