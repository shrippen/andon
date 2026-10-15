// Package feeds keeps FreshRSS's release feeds in line with the running
// stacks as a workbench (services/workbench): the update bench reads
// newer versions from them (metrics.FeedCases). Nothing is ticked and
// FreshRSS is written only after review.
//
//	pinned image without feed ─► subscribe github.com/<repo>/releases.atom
//	feed nothing runs, feed without releases ─► unsubscribe
package feeds

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"

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
	// ErrNoFeeds means the user reaches no Gitea with FreshRSS beside it.
	ErrNoFeeds = errors.New("feeds.err_none")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("feeds.err_demo")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceGitea, enums.ServiceFreshRSS}}

// The actions and fields: act_<case>=subscribe url_… category_…, or
// act_<case>=unsubscribe.
const (
	ActSubscribe   = "subscribe"
	ActUnsubscribe = "unsubscribe"
	FieldURL       = "url"
	FieldCategory  = "category"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldURL, FieldCategory}

const (
	ProblemGone workbench.Problem = "feeds.problem_gone" // settled meanwhile
	ProblemURL  workbench.Problem = "feeds.problem_url"  // no GitHub release feed
)

// feedURL is the release feed form subscribed: github.com/<repo>/releases.atom.
var feedURL = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/(releases|tags)\.atom$`)

const prefKey = "feeds"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the bench for one Verbund.
type Page struct {
	Scopes   []workbench.Scope
	Scope    string
	Choose   bool
	Cases    []metrics.FeedCase
	Category string // the release feeds' usual category, proposed for new ones
}

type book struct {
	scope    workbench.Scope
	choose   bool
	conn     *model.Connection // FreshRSS
	cases    []metrics.FeedCase
	category string
}

// Open lists the proposals.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Cases, p.Category = b.cases, b.category
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoFeeds
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
	b.conn = b.scope.Conn(enums.ServiceFreshRSS)

	holder := model.UserHolder(who.UserID)
	get := func(service enums.ServiceType) any {
		c := b.scope.Conn(service)
		res, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, fresh)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Cached)
		}
		if err != nil {
			return nil
		}
		return res.Data
	}
	gitea, _ := get(enums.ServiceGitea).(*sources.GiteaDataset)
	rss, _ := get(enums.ServiceFreshRSS).(*sources.FreshRSSDataset)
	if gitea == nil || rss == nil {
		return b, scopes, ErrNoFeeds
	}
	b.cases = metrics.FeedCases(gitea, rss)
	b.category = usualCategory(rss)
	return b, scopes, nil
}

// usualCategory is the category most release feeds are in.
func usualCategory(rss *sources.FreshRSSDataset) string {
	count := map[string]int{}
	best := ""
	for _, f := range rss.Feeds {
		if !feedURL.MatchString(f.URL) || f.Category == "" {
			continue
		}
		count[f.Category]++
		if count[f.Category] > count[best] {
			best = f.Category
		}
	}
	return best
}

// Item is a proposal as the review shows it.
type Item struct {
	metrics.FeedCase
	Category string
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
}

// Check: the case is still open; a new feed is a GitHub release feed.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.FeedCase) bool { return c.Key == s.Case })
	if i < 0 {
		return it, ProblemGone
	}
	it.FeedCase = b.cases[i]
	add := it.Action == metrics.FeedAdd
	if (add && s.Action != ActSubscribe) || (!add && s.Action != ActUnsubscribe) {
		return it, ProblemGone
	}
	if !add {
		return it, workbench.ProblemNone
	}
	it.URL, it.Category = strings.TrimSpace(s.Field(FieldURL)), strings.TrimSpace(s.Field(FieldCategory))
	if !feedURL.MatchString(it.URL) {
		return it, ProblemURL
	}
	return it, workbench.ProblemNone
}

// Review checks the proposals and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step) (Plan, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return Plan{}, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	return Plan{Plan: plan, Scope: b.scope.Key}, err
}

// Apply writes to FreshRSS against fresh data.
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
	secret, err := svcdata.Secret(ctx, d, b.conn, model.UserHolder(who.UserID))
	if err != nil {
		return workbench.Result{}, err
	}
	w := writer{book: b, to: outbound.Target{URL: b.conn.URL, Token: secret, VerifyTLS: b.conn.VerifyTLS}}
	out := workbench.Apply[Item](ctx, d, who, w, plan, "feeds", ip)
	svcdata.Forget(b.conn.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, err
}

type writer struct {
	book
	to outbound.Target
}

func (w writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	if p.Action == ActUnsubscribe {
		return p.Item.Title, outbound.FreshUnsubscribe(ctx, w.to, p.Item.Feed)
	}
	return p.Item.Repo, outbound.FreshSubscribe(ctx, w.to, p.Item.URL, "", p.Item.Category)
}

func (w writer) Name(p workbench.Planned[Item]) string { return p.Item.Repo + p.Item.Title }

func (w writer) Reason(err error) string { return err.Error() }

func (w writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"feed": p.Item.URL}
}
