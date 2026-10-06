// Package svcdata is cached access to sources.
//
//	key = sha256(source, connection, credential owner, params)
//
// A shared connection is fetched once for everybody; a connection with
// personal credentials once per user.
//
// Results live in memory for the source's TTL (typed datasets, no decoding
// needed); Force skips that. The persisted cache row only records the last
// outcome for inspection.
package svcdata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	"andon/internal/sources"
)

// Freshness selects whether a cached result within its TTL is acceptable.
type Freshness int

const (
	Cached Freshness = iota
	Force
	// Stored returns the last result the background run fetched and never
	// reaches the service; before the first run the result is Pending.
	Stored
)

// ErrMissingCredential means personal credentials are required but the
// user has none yet.
var ErrMissingCredential = errors.New("svcdata: missing personal credential")

// ErrTemplateChanged means the holder's login was entered for an older
// version of the template: it stays paused until activated again.
var ErrTemplateChanged = errors.New("svcdata: template changed")

// Result is one source fetch's outcome. Data is the source's own typed
// dataset (e.g. *sources.KimaiDataset), or nil on failure. Pending means
// no background run has fetched it yet.
type Result struct {
	Data      any
	FetchedAt time.Time
	OkAt      time.Time
	Error     string
	Pending   bool
	NextAt    time.Time // when the query may run again (see Pace)
}

// Ok reports whether the fetch succeeded.
func (r Result) Ok() bool { return r.Error == "" && r.Data != nil }

// HolderOf is the cache partition and login of a fetch: nobody for a
// fixed connection, else the holder asked for.
func HolderOf(conn *model.Connection, h model.Holder) model.Holder {
	if conn == nil || conn.CredentialMode != enums.CredentialPersonal {
		return model.NoHolder
	}
	return h
}

// Place is a space as far as logins care: where a connection lives, or
// where a widget shows it.
type Place struct {
	Kind enums.SpaceKind
	Team int64 // the team of a team space
}

// HolderAt is whose login shows a template living at from to userID on a
// widget at: a team space uses the team's login to an instance template,
// everywhere else it is the user's own.
//
//	instance template, team board      → the team's login
//	instance template, personal board  → the user's login
//	team template, team board          → the user's login
func HolderAt(from, at Place, userID int64) model.Holder {
	if from.Kind == enums.SpaceInstance && at.Kind == enums.SpaceTeam && at.Team > 0 {
		return model.TeamHolder(at.Team)
	}
	return model.UserHolder(userID)
}

func cacheKey(sourceKey string, connID *int64, owner model.Holder, params map[string]any) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]any, len(params))
	for _, k := range keys {
		sorted[k] = params[k]
	}
	raw, _ := json.Marshal([]any{sourceKey, connID, owner, sorted})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)
}

// SourceCtx builds the source context of a connection for one-off calls
// outside the cache (downloads a user asked for).
func SourceCtx(d *sql.DB, conn *model.Connection, h model.Holder) (sources.Ctx, error) {
	var sctx sources.Ctx
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		sctx, err = buildCtx(tx, conn, h, nil)
		return err
	})
	return sctx, err
}

func buildCtx(q db.Queryer, conn *model.Connection, h model.Holder, params map[string]any) (sources.Ctx, error) {
	if conn == nil {
		return sources.Ctx{Params: params}, nil
	}

	var secret string
	if conn.CredentialMode == enums.CredentialPersonal {
		s, err := heldSecret(q, conn, HolderOf(conn, h))
		if err != nil {
			return sources.Ctx{}, err
		}
		secret = s
	} else if len(conn.SecretEnc) > 0 {
		s, err := crypto.Decrypt(conn.SecretEnc, crypto.PurposeCredential)
		if err != nil {
			return sources.Ctx{}, err
		}
		secret = s
	}

	return sources.Ctx{
		URL: conn.URL, Secret: secret, VerifyTLS: conn.VerifyTLS, Options: conn.Options, Params: params,
	}, nil
}

// heldSecret is a holder's login to a template: missing without one,
// paused when entered for an older revision or another host.
func heldSecret(q db.Queryer, conn *model.Connection, h model.Holder) (string, error) {
	if h == model.NoHolder {
		return "", ErrMissingCredential
	}
	cred, err := content.Credential(q, conn.ID, h)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", ErrMissingCredential
	}
	if cred.SecretEnc == nil || cred.Revision < conn.Revision {
		return "", ErrTemplateChanged
	}
	return crypto.Decrypt(cred.SecretEnc, crypto.PurposeCredential)
}

// connVersion changes whenever what a fetch depends on changes (URL,
// secret, options), so edited connections never see older results.
func connVersion(conn *model.Connection) string {
	if conn == nil {
		return ""
	}
	raw, _ := json.Marshal([]any{conn.URL, conn.SecretEnc, conn.Options, conn.VerifyTLS})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf(":%x", sum[:8])
}

// errorTTL caps how long a failed fetch is served before retrying.
const errorTTL = time.Minute

// staleFor is how long a failed fetch still hands out the last good data:
// a service that fails once must not vanish from the analysis (its hints
// would resolve and reopen), one that stays down eventually does.
const staleFor = time.Hour

type memEntry struct {
	result  Result
	connID  int64 // 0 without a connection
	expires time.Time
	used    time.Time // last read or write, for eviction
}

var (
	memMu sync.Mutex
	mem   = map[string]memEntry{}
	// latest keeps the last result per key without expiry, for Stored
	// reads; a failed fetch keeps the last good data next to its error.
	latest = map[string]memEntry{}
)

// maxEntries bounds each in-memory cache; every parameter combination is
// its own key.
const maxEntries = 500

// idleFor drops a remembered result nobody read for a day, e.g. the
// preview of settings that were never saved.
const idleFor = 24 * time.Hour

// evict keeps m within maxEntries: idle entries go first, then the least
// recently used. Caller holds memMu.
func evict(m map[string]memEntry, now time.Time) {
	for k, e := range m {
		if now.Sub(e.used) > idleFor {
			delete(m, k)
		}
	}
	for len(m) > maxEntries {
		oldest, at := "", now
		for k, e := range m {
			if e.used.Before(at) {
				oldest, at = k, e.used
			}
		}
		delete(m, oldest)
	}
}

// Sizes reports how many results the caches hold: fresh ones (within
// their TTL) and the last known per key.
func Sizes() (fresh, known int) {
	memMu.Lock()
	defer memMu.Unlock()
	return len(mem), len(latest)
}

func remembered(key string, now time.Time) (Result, bool) {
	memMu.Lock()
	defer memMu.Unlock()

	e, ok := mem[key]
	if !ok || now.After(e.expires) {
		delete(mem, key)
		return Result{}, false
	}
	return e.result, true
}

// remember caches a fetch and returns what callers get: after a failure
// the last good data (Error set, so Ok stays false) for up to staleFor.
func remember(key string, connID int64, result Result) Result {
	memMu.Lock()
	defer memMu.Unlock()

	now := time.Now()
	for k, e := range mem {
		if now.After(e.expires) {
			delete(mem, k)
		}
	}

	kept := result
	if prev, ok := latest[key]; ok && !result.Ok() && prev.result.Data != nil {
		kept.Data, kept.OkAt = prev.result.Data, prev.result.OkAt
	}
	latest[key] = memEntry{result: kept, connID: connID, used: now}

	served := result
	if kept.Data != nil && now.Sub(kept.OkAt) <= staleFor {
		served = kept
	}
	mem[key] = memEntry{result: served, connID: connID, expires: result.NextAt, used: now}
	evict(mem, now)
	evict(latest, now)
	return served
}

func stored(key string) Result {
	memMu.Lock()
	defer memMu.Unlock()

	if e, ok := latest[key]; ok {
		e.used = time.Now()
		latest[key] = e
		return e.result
	}
	return Result{Pending: true}
}

// Forget drops every cached result of a connection, e.g. after its URL
// or credentials changed.
func Forget(connID int64) {
	memMu.Lock()
	defer memMu.Unlock()

	for k, e := range mem {
		if e.connID == connID {
			delete(mem, k)
		}
	}
	for k, e := range latest {
		if e.connID == connID {
			delete(latest, k)
		}
	}
}

// Due reports whether a Cached read of r would reach the source again:
// nothing stored yet, or past its NextAt (a result from before the pace:
// older than the source's TTL, errorTTL after a failure). A tile whose
// data is not due gains nothing from a reload.
func Due(sourceKey string, r Result, now time.Time) bool {
	source, err := sources.Get(sourceKey)
	if err != nil || r.Pending {
		return true
	}
	if !r.NextAt.IsZero() {
		return !now.Before(r.NextAt)
	}
	ttl := source.TTL()
	if !r.Ok() {
		ttl = min(ttl, errorTTL)
	}
	return now.Sub(r.FetchedAt) >= ttl
}

// Get fetches source sourceKey (never raises for a service error — it
// comes back as Result.Error) and persists the outcome to the cache table.
func Get(ctx context.Context, d *sql.DB, sourceKey string, params map[string]any, conn *model.Connection, h model.Holder, fresh Freshness) (Result, error) {
	source, err := sources.Get(sourceKey)
	if err != nil {
		return Result{}, err
	}

	owner := HolderOf(conn, h)
	var connID *int64
	if conn != nil {
		connID = &conn.ID
	}
	key := cacheKey(sourceKey, connID, owner, params) + connVersion(conn)
	if fresh == Cached {
		if result, ok := remembered(key, time.Now()); ok {
			return result, nil
		}
	}

	// A known stored result needs neither a transaction nor the secret:
	// a big board reads hundreds per view. Personal credentials still go
	// through buildCtx, which fails once the credential is gone.
	if fresh == Stored && (conn == nil || conn.CredentialMode != enums.CredentialPersonal) {
		if result := stored(key); !result.Pending {
			return result, nil
		}
	}

	var sctx sources.Ctx
	err = db.WithRead(d, func(tx *sql.Tx) error {
		sctx, err = buildCtx(tx, conn, owner, params)
		return err
	})
	if err != nil {
		return Result{}, err
	}

	// Signed-in connections: the stored grant becomes a current token, but
	// only when a fetch will actually happen.
	if sctx.Secret == sources.GrantMarker && (fresh != Stored || stored(key).Pending) {
		token, err := grantToken(ctx, d, conn, owner)
		if errors.Is(err, ErrMissingCredential) {
			return Result{}, err
		}
		if err != nil {
			return Result{FetchedAt: time.Now().UTC(), Error: err.Error()}, nil
		}
		sctx.Secret = token
	}

	if fresh == Stored {
		result := stored(key)
		if result.Pending {
			fillLater(d, key, source, sctx, conn)
		}
		return result, nil
	}

	if spent(d, conn) {
		if result := stored(key); !result.Pending {
			return result, nil
		}
		return Result{FetchedAt: time.Now().UTC(), Error: BudgetSpent}, nil
	}
	return shared(ctx, d, key, sourceKey, source, sctx, conn), nil
}

// fetches joins concurrent fetches of one key: the analysis run, a tile
// and a second viewer asking at once reach the service once.
var fetches singleflight.Group

// shared runs or joins the fetch of key.
//
// The fetch is not tied to any caller: a cancelled request must not fail
// the others waiting on it. A caller that gives up gets Pending and the
// fetch finishes for the cache.
func shared(ctx context.Context, d *sql.DB, key, sourceKey string, source sources.Source, sctx sources.Ctx, conn *model.Connection) Result {
	detached := context.WithoutCancel(ctx)
	done := fetches.DoChan(key, func() (any, error) {
		return fetch(detached, d, key, sourceKey, source, sctx, conn), nil
	})
	select {
	case out := <-done:
		return out.Val.(Result)
	case <-ctx.Done():
		return Result{Pending: true}
	}
}

// BudgetSpent is the error of a fetch skipped because the connection's
// daily budget is used up.
const BudgetSpent = "budget.spent"

// spent reports whether a rate-limited connection has used today's budget.
func spent(d *sql.DB, conn *model.Connection) bool {
	if conn == nil || conn.DailyBudget <= 0 {
		return false
	}
	n, err := data.Fetches(d, conn.ID, time.Now().UTC().Format(time.DateOnly))
	return err == nil && n >= conn.DailyBudget
}

// backgroundWait bounds a background fill.
const backgroundWait = 2 * time.Minute

var (
	inflightMu sync.Mutex
	inflight   = map[string]bool{}
)

// fillLater fetches a missing Stored value outside the request, once per
// key at a time (a new connection before the next background run).
func fillLater(d *sql.DB, key string, source sources.Source, sctx sources.Ctx, conn *model.Connection) {
	inflightMu.Lock()
	if inflight[key] {
		inflightMu.Unlock()
		return
	}
	inflight[key] = true
	inflightMu.Unlock()

	filling.Go(func() {
		defer func() {
			inflightMu.Lock()
			delete(inflight, key)
			inflightMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), backgroundWait)
		defer cancel()
		fetch(ctx, d, key, source.Key(), source, sctx, conn)
	})
}

// filling tracks background fills, so shutdown can wait for them.
var filling sync.WaitGroup

// WaitFills blocks until every background fill has finished.
func WaitFills() { filling.Wait() }

// fetch reaches the service and remembers the outcome.
func fetch(ctx context.Context, d *sql.DB, key, sourceKey string, source sources.Source, sctx sources.Ctx, conn *model.Connection) Result {
	var err error
	now := time.Now().UTC()
	if push, ok := source.(sources.PushSource); ok && conn != nil {
		if sctx.Events, err = pushedEvents(d, conn.ID, now.Add(-push.PushWindow())); err != nil {
			return Result{FetchedAt: now, Error: err.Error()}
		}
	}
	metered, usage := sources.Metered(ctx)
	out, fetchErr := source.Fetch(metered, sctx)
	took := time.Since(now).Milliseconds()
	result := Result{FetchedAt: now}
	outcome := fetchFailed
	if fetchErr != nil {
		result.Error = fetchErr.Error()
	} else {
		result.Data, result.OkAt, outcome = out, now, fetchOK
	}

	memConn := int64(0)
	if conn != nil {
		memConn = conn.ID
	}
	result.NextAt = pace.next(paceKey{conn: memConn, query: key, main: isMain(source, conn)}, ruleOf(source, conn), usage(), outcome, now)
	served := remember(key, memConn, result)
	_ = persistCache(d, key, sourceKey, result) // best-effort; a cache write failure must not fail the fetch
	if conn != nil {
		_ = data.RecordFetch(d, conn.ID, now, took, result.Error) // best-effort, health view only
	}
	return served
}

func pushedEvents(d *sql.DB, connID int64, since time.Time) ([]sources.Pushed, error) {
	events, err := data.HookEvents(d, connID, since)
	if err != nil {
		return nil, err
	}
	out := make([]sources.Pushed, 0, len(events))
	for _, e := range events {
		out = append(out, sources.Pushed{Event: e.Event, Subject: e.Subject, At: e.At})
	}
	return out, nil
}

func persistCache(d *sql.DB, key, sourceKey string, result Result) error {
	entry := &model.CacheEntry{Key: key, Source: sourceKey, FetchedAt: result.FetchedAt, Error: result.Error}
	// Outcome only: the dataset stays in memory (bank transactions, visits
	// and logins never reach the disk as a copy).
	if result.Ok() {
		okAt := result.OkAt
		entry.OkAt = &okAt
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		return data.PutCache(tx, entry)
	})
}

// Prune deletes cache entries older than the retention window.
const CacheRetention = 7 * 24 * time.Hour

// StatsRetention keeps fetch statistics for the health view and budget.
const StatsRetention = 30 * 24 * time.Hour

func Prune(d *sql.DB) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		if err := data.PruneConnStats(tx, now.Add(-StatsRetention).Format(time.DateOnly)); err != nil {
			return err
		}
		return data.PruneCache(tx, now.Add(-CacheRetention))
	})
}

// Secret returns the credential a holder would fetch conn with, for the
// few calls that act instead of read (e.g. switching a light). A
// signed-in connection's grant becomes its current access token.
func Secret(ctx context.Context, d *sql.DB, conn *model.Connection, h model.Holder) (string, error) {
	var sctx sources.Ctx
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		sctx, err = buildCtx(tx, conn, h, nil)
		return err
	})
	if err != nil || sctx.Secret != sources.GrantMarker {
		return sctx.Secret, err
	}
	return grantToken(ctx, d, conn, HolderOf(conn, h))
}
