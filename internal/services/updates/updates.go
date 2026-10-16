// Package updates brings image updates into the compose repos as a
// workbench (services/workbench): What's Up Docker finds a newer tag, or
// a release feed in FreshRSS announces a newer version of a pinned
// image; the stack gets a pull request in Gitea after review
// (metrics.UpdateCases). Merging and deploying stay with the owner.
//
//	WUD, FreshRSS ─ newer tags ─┐
//	Gitea ─ stacks ─────────────┼─► cases by urgency ─► review ─► PR per stack in Gitea
//	NVD, Pangolin ──────────────┘   (CVE, public)
package updates

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

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
	// ErrNoGitea means the user reaches no Gitea with compose stacks.
	ErrNoGitea = errors.New("updates.err_no_gitea")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("updates.err_demo")
	// errTagGone: the compose file no longer pins the running tag.
	errTagGone = errors.New("updates.problem_file")
)

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceGitea},
	Optional: []enums.ServiceType{enums.ServiceWUD, enums.ServiceFreshRSS, enums.ServiceNVD, enums.ServicePangolin}}

// The action and field: act_<case>=propose tag_<case>=1.132.3.
const (
	ActPropose = "propose"
	FieldTag   = "tag"
)

// Fields lists the fields, for the form.
var Fields = []string{FieldTag}

const (
	ProblemGone   workbench.Problem = "updates.problem_gone"   // updated meanwhile
	ProblemPinned workbench.Problem = "updates.problem_pinned" // no stack pins the tag
	ProblemTag    workbench.Problem = "updates.problem_tag"    // no new tag given
)

// branchPrefix starts the branches Andon proposes from.
const branchPrefix = "andon/update-"

const prefKey = "updates"

type state struct {
	Scope string `json:"scope,omitempty"`
	// Summaries are Claude's readings of the release notes, by case and
	// new tag ("u-regis-immich-server@v1.132.3").
	Summaries map[string]assist.Changes `json:"summaries,omitempty"`
}

// summaryKey names a summary: the case and the tag it leads to.
func summaryKey(c metrics.UpdateCase) string { return c.Key + "@" + c.NewTag }

// Page is the bench for one Verbund.
type Page struct {
	Scopes  []workbench.Scope
	Scope   string
	Choose  bool
	Cases   []metrics.UpdateCase
	Finders bool // the Verbund has WUD or FreshRSS to find newer tags
	NVD     bool
	Assist  bool                      // Claude can sum up release notes
	Changes map[string]assist.Changes // by case key, for the current new tag
}

type book struct {
	summaries map[string]assist.Changes
	scope     workbench.Scope
	choose    bool
	gitea     *model.Connection
	cases     []metrics.UpdateCase
}

// Open lists the updates.
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string) (Page, error) {
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p := Page{Scopes: scopes, Scope: b.scope.Key, Choose: b.choose}
	if err != nil {
		return p, err
	}
	p.Cases, p.NVD, p.Assist = b.cases, b.scope.Conn(enums.ServiceNVD) != nil, assist.Enabled()
	p.Changes = map[string]assist.Changes{}
	for _, c := range b.cases {
		if ch, ok := b.summaries[summaryKey(c)]; ok {
			p.Changes[c.Key] = ch
		}
	}
	p.Finders = b.scope.Conn(enums.ServiceWUD) != nil || b.scope.Conn(enums.ServiceFreshRSS) != nil
	return p, nil
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoGitea
	}
	st, err := workbench.LoadState[state](d, who, prefKey)
	if err != nil {
		return book{}, scopes, err
	}
	if scope == "" {
		scope = st.Scope
	}
	b := book{summaries: st.Summaries}
	b.scope, b.choose = workbench.Pick(scopes, scope)
	b.gitea = b.scope.Conn(enums.ServiceGitea)

	holder := model.UserHolder(who.UserID)
	get := func(service enums.ServiceType, f svcdata.Freshness) any {
		c := b.scope.Conn(service)
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
	in := metrics.UpdateInputs{}
	if in.Gitea, _ = get(enums.ServiceGitea, fresh).(*sources.GiteaDataset); in.Gitea == nil {
		return b, scopes, ErrNoGitea
	}
	in.WUD, _ = get(enums.ServiceWUD, fresh).(*sources.WUDDataset)
	in.Feeds, _ = get(enums.ServiceFreshRSS, fresh).(*sources.FreshRSSDataset)
	in.NVD, _ = get(enums.ServiceNVD, svcdata.Stored).(*sources.NVDDataset)
	if pg, ok := get(enums.ServicePangolin, svcdata.Stored).(*sources.PangolinDataset); ok {
		for _, r := range pg.Resources {
			if r.Enabled {
				in.Public = append(in.Public, strings.ToLower(r.Name), strings.ToLower(r.Domain))
			}
		}
	}
	b.cases = metrics.UpdateCases(in)
	return b, scopes, nil
}

// Item is a proposal as the review shows it.
type Item struct {
	metrics.UpdateCase
	To      string // the tag to write
	Changes *assist.Changes
}

// Plan is the review.
type Plan struct {
	workbench.Plan[Item]
	Scope string
}

// Check: the update is still open, a stack pins the running tag and a
// new tag is given.
func (b book) Check(s *workbench.Step) (Item, workbench.Problem) {
	var it Item
	i := slices.IndexFunc(b.cases, func(c metrics.UpdateCase) bool { return c.Key == s.Case })
	if i < 0 || s.Action != ActPropose {
		return it, ProblemGone
	}
	it.UpdateCase = b.cases[i]
	if !it.Pinned {
		return it, ProblemPinned
	}
	if ch, ok := b.summaries[summaryKey(it.UpdateCase)]; ok {
		it.Changes = &ch
	}
	it.To = strings.TrimSpace(s.Field(FieldTag))
	if it.To == "" || it.To == it.Tag {
		return it, ProblemTag
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

// Apply opens the pull requests against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, scope string, steps []workbench.Step, ip string) (workbench.Result, []string, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Force)
	if err != nil {
		return workbench.Result{}, nil, err
	}
	plan, err := workbench.PlanOf[Item](b, steps)
	if err != nil {
		return workbench.Result{}, nil, err
	}
	w := &writer{book: b}
	if plan.Writes() > 0 {
		if err := connections.Writable(d, who, b.gitea.ID); err != nil {
			return workbench.Result{}, nil, err
		}
		if sources.IsDemo(b.gitea.URL) {
			return workbench.Result{}, nil, ErrDemo
		}
		token, err := svcdata.Secret(ctx, d, b.gitea, model.UserHolder(who.UserID))
		if err != nil {
			return workbench.Result{}, nil, err
		}
		w.to = outbound.Target{URL: b.gitea.URL, Token: token, VerifyTLS: b.gitea.VerifyTLS}
	}
	out := workbench.Apply[Item](ctx, d, who, w, plan, "updates", ip)
	svcdata.Forget(b.gitea.ID)
	err = workbench.SaveState(d, who, prefKey, func(s *state) { s.Scope = b.scope.Key })
	return out, w.opened, err
}

type writer struct {
	book
	to     outbound.Target
	opened []string // the pull requests' pages
}

// Write changes the tag in the stack's compose file on a new branch and
// opens a pull request.
func (w *writer) Write(ctx context.Context, p workbench.Planned[Item]) (string, error) {
	it := p.Item
	// The image as the compose file writes it: What's Up Docker says
	// "library/nginx" where the file says "nginx".
	image := cmp.Or(it.Written, it.Image)
	old, next := image+":"+it.Tag, image+":"+it.To
	where := it.Stack.Name + "/" + it.Service
	page, err := outbound.GiteaPropose(ctx, w.to, outbound.GiteaChange{
		Repo: it.Stack.Repo, Path: it.Stack.Path, Branch: branchPrefix + strings.TrimPrefix(it.Key, "u-") + "-" + it.To,
		Message: fmt.Sprintf("Update %s to %s", where, it.To), Title: fmt.Sprintf("%s: %s → %s", where, it.Tag, it.To),
		Body: prBody(it),
		Change: func(content []byte) ([]byte, error) {
			if !strings.Contains(string(content), old) {
				return nil, errTagGone
			}
			return []byte(strings.ReplaceAll(string(content), old, next)), nil
		},
	})
	if err != nil {
		return "", err
	}
	w.opened = append(w.opened, page)
	return page, nil
}

// prBody says why the update was proposed.
func prBody(it Item) string {
	lines := []string{fmt.Sprintf("Proposed by Andon: What's Up Docker found `%s` for `%s` (running `%s`).", it.To, it.Image, it.Tag)}
	if it.Release != "" {
		lines[0] = fmt.Sprintf("Proposed by Andon: a newer release of `%s` (running `%s`): %s", it.Image, it.Tag, it.Release)
	}
	if len(it.CVEs) > 0 {
		lines = append(lines, "Running tag affected by: "+strings.Join(it.CVEs, ", "))
	}
	if ch := it.Changes; ch != nil {
		for _, part := range []struct {
			title string
			items []string
		}{{"Breaking", ch.Breaking}, {"Security", ch.Security}, {"Notable", ch.Notable}} {
			if len(part.items) > 0 {
				lines = append(lines, "**"+part.title+"** (summed up by Claude)\n- "+strings.Join(part.items, "\n- "))
			}
		}
	}
	return strings.Join(lines, "\n\n")
}

func (w *writer) Name(p workbench.Planned[Item]) string { return p.Item.Image }

func (w *writer) Reason(err error) string { return err.Error() }

func (w *writer) Audit(p workbench.Planned[Item]) map[string]any {
	return map[string]any{"image": p.Item.Image, "from": p.Item.Tag, "to": p.Item.To, "repo": p.Item.Stack.Repo}
}

// ErrNoNotes means the case's project is unknown on GitHub (a WUD case
// without release feed) or it has no notes in the range.
var ErrNoNotes = errors.New("updates.err_no_notes")

// releaseRepo reads the GitHub repo of a release page.
var releaseRepo = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/releases/`)

// Summarize lets Claude sum up the release notes between the running
// and the new tag of one case, and keeps it for the page and the PR.
func Summarize(ctx context.Context, d *sql.DB, who *access.Principal, scope, key string) error {
	b, _, err := load(ctx, d, who, scope, svcdata.Cached)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(b.cases, func(c metrics.UpdateCase) bool { return c.Key == key })
	if i < 0 {
		return ErrNoNotes
	}
	c := b.cases[i]
	m := releaseRepo.FindStringSubmatch(c.Release)
	if m == nil {
		return ErrNoNotes
	}
	notes, err := sources.ReleaseNotes(ctx, m[1], c.Tag, c.NewTag)
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return ErrNoNotes
	}
	texts := make([]string, len(notes))
	for i, n := range notes {
		texts[i] = "## " + n.Tag + "\n" + n.Body
	}
	ch, err := assist.SummarizeChanges(ctx, who.Locale, c.Image, c.Tag, c.NewTag, texts)
	if err != nil {
		return err
	}
	return workbench.SaveState(d, who, prefKey, func(s *state) {
		s.Scope = b.scope.Key
		if s.Summaries == nil {
			s.Summaries = map[string]assist.Changes{}
		}
		s.Summaries[summaryKey(c)] = ch
	})
}
