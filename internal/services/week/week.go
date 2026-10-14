// Package week closes a week in Kimai as a workbench (services/workbench):
// work other services saw (metrics.WeekGaps: Dawarich visits at a
// customer, appointments naming one) becomes Kimai entries after review.
//
//	Dawarich ─visits──┐
//	calendar ─events──┼─► gaps ─► project, activity, times proposed ─► review ─► Kimai
//	Kimai ──entries───┘   (the customer's latest project and activity)
package week

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
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoKimai means the user reaches no Kimai.
	ErrNoKimai = errors.New("closeweek.err_no_kimai")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("closeweek.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceKimai},
	Optional: []enums.ServiceType{enums.ServiceDawarich, enums.ServiceCalendar}}

// The action and fields: act_<gap>=book project_… activity_… begin_…
// end_… note_…; times as the form's "2026-09-08T10:00", local.
const (
	ActBook       = "book"
	FieldProject  = "project"
	FieldActivity = "activity"
	FieldBegin    = "begin"
	FieldEnd      = "end"
	FieldNote     = "note"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldProject, FieldActivity, FieldBegin, FieldEnd, FieldNote}

const (
	ProblemGone   workbench.Problem = "closeweek.problem_gone"   // booked meanwhile
	ProblemTarget workbench.Problem = "closeweek.problem_target" // project or activity missing
	ProblemTimes  workbench.Problem = "closeweek.problem_times"  // begin not before end
)

const (
	formTime   = "2006-01-02T15:04"
	kimaiTime  = "2006-01-02T15:04:05"
	weeksShown = 4
	catalogKey = "kimai.catalog"
	prefKey    = "week"
)

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Case is a gap with its proposal.
type Case struct {
	metrics.WeekGap
	Customer          string
	Project, Activity int64 // proposed, 0 none
	Checked           bool  // proposal complete: ticked
}

// Page is the bench for one Verbund and week.
type Page struct {
	Scopes  []workbench.Scope
	Scope   string
	Choose  bool
	Week    time.Time // its Monday
	Weeks   []time.Time
	Cases   []Case
	Catalog *sources.KimaiCatalog
	Sources []enums.ServiceType // what fed the gaps
}

type book struct {
	scope   workbench.Scope
	choose  bool
	conn    *model.Connection
	kimai   *sources.KimaiDataset
	catalog *sources.KimaiCatalog
	gaps    []metrics.WeekGap
	fed     []enums.ServiceType
}

// Open builds the page for the week holding day (zero: last week).
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, day, today time.Time) (Page, error) {
	p := Page{Week: metrics.WeekStart(metrics.Today(today)).AddDate(0, 0, -7)}
	if !day.IsZero() {
		p.Week = metrics.WeekStart(metrics.Today(day))
	}
	for i := 0; i < weeksShown; i++ {
		p.Weeks = append(p.Weeks, metrics.WeekStart(metrics.Today(today)).AddDate(0, 0, -7*i))
	}
	b, scopes, err := load(ctx, d, who, scope, p.Week, svcdata.Stored)
	p.Scopes, p.Scope, p.Choose = scopes, b.scope.Key, b.choose
	if err != nil {
		return p, err
	}
	p.Catalog, p.Sources = b.catalog, b.fed
	names := metrics.KimaiCustomerNames(b.kimai)
	proposals := metrics.WeekProposals(b.kimai)
	for _, g := range b.gaps {
		c := Case{WeekGap: g, Customer: names[g.CustomerID]}
		prop := proposals[g.CustomerID]
		c.Project, c.Activity = prop.ProjectID, activityID(b.catalog, prop)
		c.Checked = c.Project > 0 && c.Activity > 0
		p.Cases = append(p.Cases, c)
	}
	return p, nil
}

// activityID finds the proposed activity by name in the catalog: the
// project's own first, else a global one.
func activityID(cat *sources.KimaiCatalog, p metrics.WeekProposal) int64 {
	if cat == nil || p.Activity == "" {
		return 0
	}
	var global int64
	for _, a := range cat.Activities {
		if !strings.EqualFold(a.Name, p.Activity) {
			continue
		}
		if a.ProjectID == p.ProjectID {
			return a.ID
		}
		if a.ProjectID == 0 {
			global = a.ID
		}
	}
	return global
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, week time.Time, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoKimai
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
	b.conn = b.scope.Conn(enums.ServiceKimai)
	holder := model.UserHolder(who.UserID)
	get := func(c *model.Connection, key string, f svcdata.Freshness) any {
		res, err := svcdata.Get(ctx, d, key, nil, c, holder, f)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, key, nil, c, holder, svcdata.Cached)
		}
		if err != nil {
			return nil
		}
		return res.Data
	}
	if b.kimai, _ = get(b.conn, sources.DataKey(enums.ServiceKimai), fresh).(*sources.KimaiDataset); b.kimai == nil {
		return b, scopes, ErrNoKimai
	}
	b.catalog, _ = get(b.conn, catalogKey, svcdata.Cached).(*sources.KimaiCatalog)

	var geo *sources.DawarichDataset
	var areas map[string]metrics.AreaMapping
	if c := b.scope.Conn(enums.ServiceDawarich); c != nil {
		if geo, _ = get(c, sources.DataKey(enums.ServiceDawarich), svcdata.Stored).(*sources.DawarichDataset); geo != nil {
			areas = metrics.AreaMap(geo, b.kimai, c.Options)
			b.fed = append(b.fed, enums.ServiceDawarich)
		}
	}
	var cal *sources.CalendarResult
	if c := b.scope.Conn(enums.ServiceCalendar); c != nil {
		if cal, _ = get(c, sources.DataKey(enums.ServiceCalendar), svcdata.Stored).(*sources.CalendarResult); cal != nil {
			b.fed = append(b.fed, enums.ServiceCalendar)
		}
	}
	b.gaps = metrics.WeekGaps(b.kimai, geo, areas, cal, week, time.Local)
	return b, scopes, nil
}

// Item is an entry as the review shows it.
type Item struct {
	Gap               metrics.WeekGap
	Customer          string
	Project, Activity string // names
	ProjectID         int64
	ActivityID        int64
	Begin, End        time.Time
	Note              string
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
	Week  time.Time
}

// Check: the gap is still open, project and activity given, times sane.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.gaps, func(g metrics.WeekGap) bool { return g.Key == s.Case })
	if i < 0 || s.Action != ActBook {
		return it, ProblemGone
	}
	it.Gap, it.Customer = b.gaps[i], metrics.KimaiCustomerNames(b.kimai)[b.gaps[i].CustomerID]
	it.ProjectID, _ = strconv.ParseInt(s.Field(FieldProject), 10, 64)
	it.ActivityID, _ = strconv.ParseInt(s.Field(FieldActivity), 10, 64)
	it.Note = strings.TrimSpace(s.Field(FieldNote))
	if b.catalog != nil {
		for _, p := range b.catalog.Projects {
			if p.ID == it.ProjectID {
				it.Project = p.Name
			}
		}
		for _, a := range b.catalog.Activities {
			if a.ID == it.ActivityID {
				it.Activity = a.Name
			}
		}
	}
	if it.ProjectID <= 0 || it.ActivityID <= 0 {
		return it, ProblemTarget
	}
	var err1, err2 error
	it.Begin, err1 = time.ParseInLocation(formTime, s.Field(FieldBegin), time.Local)
	it.End, err2 = time.ParseInLocation(formTime, s.Field(FieldEnd), time.Local)
	if err1 != nil || err2 != nil || !it.Begin.Before(it.End) {
		return it, ProblemTimes
	}
	return it, workbench.ProblemNone
}

// Review checks the entries and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, week time.Time, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, week, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key, Week: week}, err
}

// Apply books the confirmed entries against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, week time.Time, steps []workbench.Step, ip string) (workbench.Result, error) {
	b, _, err := load(ctx, d, who, scope, week, svcdata.Force)
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
	out := workbench.Apply[Item](ctx, d, who, w, plan, "week", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to outbound.Target
}

func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	it := p.Item
	return it.Customer, outbound.KimaiCreate(ctx, w.to, outbound.KimaiSheet{Project: it.ProjectID, Activity: it.ActivityID,
		Begin: it.Begin.Format(kimaiTime), End: it.End.Format(kimaiTime), Description: it.Note})
}

func (w writer) Name(p workbench.Planned[Item]) string {
	return strings.TrimSpace(p.Item.Customer + " " + p.Item.Gap.Title)
}

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"day": p.Item.Begin.Format(time.DateOnly), "minutes": strconv.Itoa(int(p.Item.End.Sub(p.Item.Begin).Minutes())), "project": p.Item.Project}
}
