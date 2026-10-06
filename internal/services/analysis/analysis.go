// Package analysis runs all rules and reconciles hints (scheduler job,
// every 5 minutes).
//
//	for each space (its own connections, a team's also the instance
//	                templates the team activated)
//	  for each owner (nil = shared data, user id = personal credentials)
//	    datasets  ← <service>.data per connection (cached, 10 min)
//	    service rules  → hints (per connection)
//	    cross + deadline rules → hints (per space and owner)
package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	linkrepo "andon/internal/repos/links"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/linkstatus"
	"andon/internal/services/scheduler"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

// JobName is the scheduler job that runs RunAll: it fetches every
// integration; page views only read its results.
const JobName = "analysis"

const (
	connectorRule = "system.connector_down"
	linkType      = "link"
)

// scope is one space's datasets for one credential owner. datasets
// holds the space-wide inputs (failures, links, clocks, history) and,
// last one wins, every service's; groups hold each Verbund's own.
type scope struct {
	spaceID  int64
	owner    *int64
	settings map[string]any
	datasets map[string]any
	options  map[string]map[string]any
	fetched  []run
	groups   []group
	vague    []string // services with several free connections
}

// group is one Verbund's inputs: the space-wide ones plus its members'.
type group struct {
	conns    map[int64]bool
	datasets map[string]any
	options  map[string]map[string]any
}

func newScope(spaceID int64, owner *int64, settings map[string]any) *scope {
	return &scope{spaceID: spaceID, owner: owner, settings: settings, datasets: map[string]any{}, options: map[string]map[string]any{}}
}

// RunAll runs every rule against every space's connections and reconciles
// hints. Returns the number of hints that are new or reopened (used to
// decide whether to notify).
func RunAll(ctx context.Context, d *sql.DB, today time.Time) (int, error) {
	var spaces []*model.Space
	var conns []*model.Connection
	var holders map[int64][]model.Holder

	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		spaces, err = content.AllSpaces(tx)
		if err != nil {
			return err
		}
		conns, err = content.AllConnections(tx)
		if err != nil {
			return err
		}
		holders, err = content.CredentialHolders(tx)
		return err
	})
	if err != nil {
		return 0, err
	}

	warm(ctx, d, conns, holders)
	fresh := 0
	for _, sp := range spaces {
		n, err := runSpace(ctx, d, sp, connectionsOf(conns, holders, sp), holders, today)
		if err != nil {
			slog.Error("analysis: space failed", "space", sp.ID, "err", err)
		}
		if err := recordLight(d, sp.ID, time.Now().UTC()); err != nil {
			slog.Error("analysis: light failed", "space", sp.ID, "err", err)
		}
		fresh += n
	}
	syncBackup(d, today)
	return fresh, nil
}

// warmWorkers bounds the parallel fetches of warm.
const warmWorkers = 4

// target is one connection fetched with one holder's login.
type target struct {
	conn   *model.Connection
	holder model.Holder
}

// warm fetches every connection in parallel before the spaces run one
// by one, so they read from memory. The first run at start thus fills
// the results widgets show (Stored) in the time of the slowest service,
// not of all services together. Certificate checks wait for their hosts
// in runSpace.
func warm(ctx context.Context, d *sql.DB, conns []*model.Connection, holders map[int64][]model.Holder) {
	jobs := make(chan target)
	var wg sync.WaitGroup
	for range warmWorkers {
		wg.Go(func() {
			for t := range jobs {
				_, _ = svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(t.conn.Service)), nil, t.conn, t.holder, svcdata.Cached) // errors surface in runSpace
			}
		})
	}

	for _, conn := range conns {
		if conn.Service == string(enums.ServiceCerts) {
			continue
		}
		for _, h := range holdersOf(conn, holders[conn.ID]) {
			select {
			case jobs <- target{conn: conn, holder: h}:
			case <-ctx.Done():
			}
		}
	}
	close(jobs)
	wg.Wait()
}

// run is one fetched connection for one credential owner.
type run struct {
	conn   *model.Connection
	login  login
	owner  *int64
	result svcdata.Result
}

// runSpace fetches every connection first, then evaluates: rules see all
// datasets and failures of the space, so one outage becomes one hint.
func runSpace(ctx context.Context, d *sql.DB, sp *model.Space, mine []*model.Connection, holders map[int64][]model.Holder, today time.Time) (int, error) {
	settings := sp.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	links, err := spaceLinks(d, sp.ID)
	if err != nil {
		slog.Error("analysis: links failed", "space", sp.ID, "err", err)
	}

	scopes := map[int64]*scope{0: newScope(sp.ID, nil, settings)}
	scopeOf := func(owner *int64) *scope {
		key := ownerID(owner)
		if _, ok := scopes[key]; !ok {
			scopes[key] = newScope(sp.ID, owner, settings)
		}
		return scopes[key]
	}
	// The shared scope always runs: rules on the settings alone (tax
	// deadlines) belong there, once, not in each owner's scope too.
	scopeOf(nil)

	// Certificate checks go last: they may pick up hosts found by others.
	var runs []run
	for _, conn := range certsLast(mine) {
		for _, l := range loginsOf(conn, holders[conn.ID], sp) {
			owner := l.owner
			sc := scopeOf(owner)
			target := conn
			if conn.Service == string(enums.ServiceCerts) {
				target = withAutoHosts(conn, links, sc.datasets)
			}
			result, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(conn.Service)), nil, target, l.holder, svcdata.Cached)
			if errors.Is(err, svcdata.ErrMissingCredential) || errors.Is(err, svcdata.ErrTemplateChanged) {
				continue
			}
			if err != nil {
				slog.Error("analysis: connection failed", "connection", conn.Name, "err", err)
				failed, _ := sc.datasets[rules.FailedDataset].([]rules.Failed)
				sc.datasets[rules.FailedDataset] = append(failed, rules.Failed{Service: conn.Service, Name: conn.Name, Host: rules.HostOf(conn.URL)})
				continue
			}
			runs = append(runs, run{conn: conn, login: l, owner: owner, result: result})
			if result.Data != nil {
				sc.fetched = append(sc.fetched, runs[len(runs)-1])
				sc.datasets[conn.Service] = result.Data
				sc.options[conn.Service] = conn.Options
			}
			if !result.Ok() {
				failed, _ := sc.datasets[rules.FailedDataset].([]rules.Failed)
				sc.datasets[rules.FailedDataset] = append(failed, rules.Failed{Service: conn.Service, Name: conn.Name, Host: rules.HostOf(conn.URL)})
			}
		}
	}
	clocks := clocksOf(mine)
	for _, sc := range scopes {
		sc.datasets[rules.LinksDataset] = links
		sc.datasets[rules.ClockDataset] = clocks
	}
	if err := addSecrets(d, sp, mine, holders, scopeOf); err != nil {
		slog.Error("analysis: secrets failed", "space", sp.ID, "err", err)
	}
	for _, sc := range scopes {
		history, err := recordHistory(d, sc, time.Now().UTC())
		if err != nil {
			slog.Error("analysis: history failed", "space", sp.ID, "err", err)
			continue
		}
		sc.datasets[metrics.HistoryDataset] = history
	}

	stored, err := linkrepo.All(d)
	if err != nil {
		slog.Error("analysis: verbünde failed", "space", sp.ID, "err", err)
	}
	customerLinks := func(g verbund.Group) map[string]any {
		out := map[string]any{}
		conn := func(s enums.ServiceType) int64 {
			if c := g.Conns[string(s)]; c != nil {
				return c.ID
			}
			return 0
		}
		ninja := conn(enums.ServiceInvoiceNinja)
		if m, err := verbund.ClientMapFor(d, conn(enums.ServiceKimai), ninja); err != nil {
			slog.Error("analysis: customer links failed", "space", sp.ID, "err", err)
		} else if m != nil {
			out[rules.ClientMapDataset] = m
		}
		if m, err := verbund.PayerMapFor(d, conn(enums.ServiceSure), ninja); err != nil {
			slog.Error("analysis: payer links failed", "space", sp.ID, "err", err)
		} else if m != nil {
			out[rules.PayerMapDataset] = m
		}
		return out
	}
	for _, sc := range scopes {
		sc.split(stored, customerLinks)
	}

	fresh := 0
	for _, r := range runs {
		n, err := evaluate(d, r, scopeOf(r.owner), settings, today)
		if err != nil {
			return fresh, err
		}
		fresh += n
	}
	for _, sc := range scopes {
		n, err := runScope(d, sc, today)
		if err != nil {
			return fresh, err
		}
		fresh += n
	}
	return fresh, nil
}

// clocksOf lists the clock skew of every host the space's connections
// answered from, one entry per host.
func clocksOf(conns []*model.Connection) []rules.Clock {
	seen := map[string]bool{}
	var out []rules.Clock
	for _, conn := range conns {
		host := rules.HostOf(conn.URL)
		if host == "" || seen[host] {
			continue
		}
		skew, ok := sources.ClockSkew(host)
		if !ok {
			continue
		}
		seen[host] = true
		out = append(out, rules.Clock{Name: conn.Name, Host: host, Seconds: skew.Seconds()})
	}
	return out
}

// addSecrets lists each scope's stored secrets for the token rule: the
// shared token, or the owner's own personal one.
func addSecrets(d *sql.DB, sp *model.Space, conns []*model.Connection, holders map[int64][]model.Holder, scopeOf func(*int64) *scope) error {
	for _, conn := range conns {
		for _, l := range loginsOf(conn, holders[conn.ID], sp) {
			owner := l.owner
			c := rules.Conn{Name: conn.Name, SecretAt: conn.SecretAt, Expires: conn.SecretExpires}
			if l.holder != model.NoHolder {
				cred, err := content.Credential(d, conn.ID, l.holder)
				if err != nil {
					return err
				}
				c.SecretAt = time.Time{}
				if cred != nil {
					c.SecretAt = cred.SecretAt
				}
			}
			sc := scopeOf(owner)
			list, _ := sc.datasets[rules.ConnsDataset].([]rules.Conn)
			sc.datasets[rules.ConnsDataset] = append(list, c)
		}
	}
	return nil
}

// certsLast orders certificate connections after all others.
func certsLast(conns []*model.Connection) []*model.Connection {
	out := make([]*model.Connection, 0, len(conns))
	var certs []*model.Connection
	for _, c := range conns {
		if c.Service == string(enums.ServiceCerts) {
			certs = append(certs, c)
			continue
		}
		out = append(out, c)
	}
	return append(out, certs...)
}

// evaluate turns one fetched connection into hints.
func evaluate(d *sql.DB, r run, sc *scope, settings map[string]any, today time.Time) (int, error) {
	envs := sc.envsOf(r, settings, today)
	env := envs[0]
	outages := rules.Outages(env)

	// A connection on a host that is down as a whole is part of the outage hint.
	var down []rules.Finding
	failing := reportDown(fetcher{r.conn.ID, int64(r.login.holder)}, r.result.Ok())
	if _, inOutage := outages[rules.OutageRoot(env, rules.HostOf(r.conn.URL))]; failing && !inOutage {
		down = []rules.Finding{downFinding(r.conn, r.result.Error)}
	}
	fresh, err := syncHints(d, sc.spaceID, r.owner, &r.conn.ID, []string{connectorRule}, down)
	if err != nil || r.result.Data == nil {
		return fresh, err
	}

	// Stale data (a failed fetch) keeps the hints but is no reading for today.
	if r.result.Ok() {
		if err := snapshot(d, r.conn, r.login.holder, r.result.Data, today); err != nil {
			slog.Error("analysis: snapshot failed", "connection", r.conn.Name, "err", err)
		}
	}
	findings, ids := applyAll(rules.ForScope(r.conn.Service), r.result.Data, envs)
	kept := findings[:0]
	for _, f := range findings {
		if !rules.Suppressed(f, env, outages) {
			kept = append(kept, f)
		}
	}
	n, err := syncHints(d, sc.spaceID, r.owner, &r.conn.ID, ids, kept)
	return fresh + n, err
}

// spaceLinks returns the space's link tiles, for rules that compare them
// with bookmarks (Linkwarden) or monitors (Uptime Kuma).
func spaceLinks(d *sql.DB, spaceID int64) ([]rules.Link, error) {
	widgets, err := content.Widgets(d, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	clicks, err := data.LastClicks(d)
	if err != nil {
		return nil, err
	}
	var links []rules.Link
	for _, w := range widgets {
		target, _ := w.Config["url"].(string)
		if w.Type == linkType && target != "" {
			links = append(links, rules.Link{Title: w.Title, URL: target, DownDays: linkstatus.DownDays(d, w.ID, time.Now().UTC()),
				LastClick: clicks[w.ID]})
		}
	}
	return links, nil
}

// connectionsOf lists what a space's run fetches: its own connections,
// and in a team space the templates the team activated for itself.
func connectionsOf(conns []*model.Connection, holders map[int64][]model.Holder, sp *model.Space) []*model.Connection {
	var out []*model.Connection
	for _, c := range conns {
		if c.SpaceID == sp.ID || teamHolds(holders[c.ID], sp) {
			out = append(out, c)
		}
	}
	return out
}

// login is one fetch of a connection in a space: whose login it uses
// (holder) and whose hints its result feeds (owner, nil = the space's).
type login struct {
	holder model.Holder
	owner  *int64
}

// loginsOf lists the fetches of conn in space sp:
//
//	fixed connection                    once, the space's hints
//	template in its own space           once per user who activated it
//	instance template in a team space   once with the team's login
func loginsOf(conn *model.Connection, holders []model.Holder, sp *model.Space) []login {
	if conn.CredentialMode != enums.CredentialPersonal {
		return []login{{}}
	}
	var out []login
	for _, h := range holders {
		if conn.SpaceID != sp.ID {
			if sp.TeamID != nil && h.Team() == *sp.TeamID {
				out = append(out, login{holder: h})
			}
			continue
		}
		if uid := h.User(); uid > 0 {
			out = append(out, login{holder: h, owner: &uid})
		}
	}
	return out
}

// teamHolds reports whether sp is a team space whose team activated a
// template for itself.
func teamHolds(holders []model.Holder, sp *model.Space) bool {
	if sp.TeamID == nil {
		return false
	}
	for _, h := range holders {
		if h.Team() == *sp.TeamID {
			return true
		}
	}
	return false
}

// holdersOf lists every login conn is fetched with.
func holdersOf(conn *model.Connection, holders []model.Holder) []model.Holder {
	if conn.CredentialMode != enums.CredentialPersonal {
		return []model.Holder{model.NoHolder}
	}
	return holders
}

// snapshot stores one value per metric and day, for the trend widget.
func snapshot(d *sql.DB, conn *model.Connection, h model.Holder, dataset any, today time.Time) error {
	values := map[string]float64{}
	switch conn.Service {
	case "invoiceninja":
		stats := metrics.NinjaSummaryOf(dataset.(*sources.NinjaDataset), today, "", "")
		values["revenue_ytd"] = stats.RevenueYTD
		values["open_amount"] = stats.OpenAmount
	case "kimai":
		stats := metrics.KimaiSummaryOf(dataset.(*sources.KimaiDataset), today)
		values["month_min"] = float64(stats.MonthMin)
	}
	if len(values) == 0 {
		return nil
	}

	scope := fmt.Sprintf("%d:%d", conn.ID, int64(h))
	day := today.Format("2006-01-02")
	return db.WithTx(d, func(tx *sql.Tx) error {
		for metric, value := range values {
			if err := data.PutPoint(tx, scope, metric, day, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func runScope(d *sql.DB, sc *scope, today time.Time) (int, error) {
	var envs []rules.Env
	for _, g := range sc.groups {
		envs = append(envs, rules.Env{Today: today, Settings: sc.settings, Datasets: g.datasets, Options: g.options})
	}
	specs := rules.ForScope(rules.Cross)
	if sc.owner == nil {
		specs = append(specs, rules.ForScope(rules.Deadlines)...)
	}
	findings, ids := applyAll(specs, nil, envs)
	if sc.owner == nil {
		findings = append(findings, vagueFindings(sc.vague)...)
		ids = append(ids, vagueRule)
	}
	if sc.owner != nil {
		// Listed without findings: copies an owner's scope once made resolve.
		for _, spec := range rules.ForScope(rules.Deadlines) {
			ids = append(ids, spec.ID)
		}
	}
	return syncHints(d, sc.spaceID, sc.owner, nil, ids, findings)
}

// applyAll runs specs in every Verbund's env and merges the findings
// (one per rule and fingerprint) and the rules that ran.
func applyAll(specs []rules.Spec, dataset any, envs []rules.Env) ([]rules.Finding, []string) {
	var findings []rules.Finding
	var ids []string
	seen := map[string]bool{}
	ran := map[string]bool{}
	for _, env := range envs {
		found, listed := apply(specs, dataset, env)
		for _, id := range listed {
			if !ran[id] {
				ran[id] = true
				ids = append(ids, id)
			}
		}
		for _, f := range found {
			key := f.Rule + "\x00" + f.Fingerprint
			if !seen[key] {
				seen[key] = true
				findings = append(findings, f)
			}
		}
	}
	return findings, ids
}

func apply(specs []rules.Spec, dataset any, env rules.Env) ([]rules.Finding, []string) {
	var findings []rules.Finding
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		// Without its inputs a rule's hints stay: not listed, not resolved.
		if spec.Incomplete(env) {
			continue
		}
		cfg := rules.Config(spec, env.Settings)
		ids = append(ids, spec.ID)
		if enabled, ok := cfg[rules.Enabled].(bool); ok && !enabled {
			continue
		}
		escalate := rules.EscalateDays(cfg)
		for _, f := range rules.Filter(safeRun(spec, dataset, cfg, env), cfg) {
			f.EscalateDays = escalate
			findings = append(findings, f)
		}
	}
	return findings, ids
}

// safeRun isolates one rule's panic (a broken rule must not stop the others
// or take the analysis job down).
func safeRun(spec rules.Spec, dataset any, cfg map[string]any, env rules.Env) (out []rules.Finding) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("analysis: rule panicked", "rule", spec.ID, "recover", r)
			out = nil
		}
	}()
	return spec.Run(dataset, cfg, env)
}

// downAfter is how many runs in a row must fail before a connection is
// reported down: one slow answer (Kimai at 7 % failures) is no outage.
const downAfter = 2

// fetcher is one connection fetched for one owner (0 = shared).
type fetcher struct{ conn, owner int64 }

var (
	failMu     sync.Mutex
	failStreak = map[fetcher]int{} // failed runs in a row
)

// reportDown counts a fetcher's failed runs in a row and tells whether
// its connection now counts as down.
func reportDown(f fetcher, ok bool) bool {
	failMu.Lock()
	defer failMu.Unlock()
	if ok {
		delete(failStreak, f)
		return false
	}
	failStreak[f]++
	return failStreak[f] >= downAfter
}

func downFinding(conn *model.Connection, errMsg string) rules.Finding {
	if errMsg == "" {
		errMsg = "?"
	}
	// A source error may be a catalog key; the reader gets its text.
	var errParam any = errMsg
	if i18n.Has(errMsg) {
		errParam = map[string]any{"$t": errMsg}
	}
	return rules.Finding{
		Fingerprint: fmt.Sprintf("down:%d", conn.ID), Rule: connectorRule, Severity: enums.SeverityWarn,
		Message: "system.connector_down", Params: map[string]any{"name": conn.Name, "error": errParam},
		Sources: []string{conn.Service},
	}
}

func syncHints(d *sql.DB, spaceID int64, owner *int64, connID *int64, ruleIDs []string, findings []rules.Finding) (int, error) {
	var n int
	err := db.WithTx(d, func(tx *sql.Tx) error {
		var err error
		n, err = hints.Sync(tx, spaceID, owner, connID, ruleIDs, findings)
		return err
	})
	return n, err
}

// ErrNotScheduled means no scheduler runs the analysis (e.g. disabled).
var ErrNotScheduled = errors.New("analysis.not_scheduled")

// RequestRun asks the scheduler for a run now. Admins only.
func RequestRun(who *access.Principal) error {
	if !who.IsAdmin() {
		return access.ErrDenied
	}
	if !scheduler.Trigger(JobName) {
		return ErrNotScheduled
	}
	return nil
}

// LastRun reports the latest background run, false before the first.
func LastRun() (scheduler.Run, bool) {
	return scheduler.LastRun(JobName)
}
