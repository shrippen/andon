package svcdata

// Pace: when a query may reach its service again. Every fetch is metered
// (sources.Metered); its calls and the service's rate-limit answers set
// the next fetch:
//
//	rule    base = the source's TTL (the service's default), or the
//	             connection's interval for its main query (manual)
//	refused 429 / quota gone → wait for Retry-After or the renewal, else
//	             double the last wait (up to paceMaxBackoff); manual too
//	tight   quota told       → spread what is left above paceReserve until
//	             the renewal (automatic only):
//	             every = time to renewal · calls per fetch / spendable calls
//	failed  other error      → retry after errorTTL
//
//	fetch ─► Usage ─► pacer.next ─► Result.NextAt ─► cache expiry, Due

import (
	"sync"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/sources"
)

// PaceReason says why a query runs at its interval.
type PaceReason string

const (
	PaceNone    PaceReason = ""        // not fetched yet
	PaceDefault PaceReason = "default" // the service's interval
	PaceManual  PaceReason = "manual"  // the connection's own interval
	PaceTight   PaceReason = "tight"   // stretched to last until the quota renews
	PaceRefused PaceReason = "refused" // the service refused for its rate limit
)

// Pace is how often a query runs and why.
type Pace struct {
	Every  time.Duration
	Until  time.Time // refused: no fetch before; zero otherwise
	Reason PaceReason
}

// fetchOutcome tells pacer.next whether the fetch got its data.
type fetchOutcome int

const (
	fetchOK fetchOutcome = iota
	fetchFailed
)

const (
	// paceReserve is the share of a quota left for what a person does:
	// detail dialogs, connection tests.
	paceReserve = 0.2
	// paceMaxBackoff caps the wait after refusals without Retry-After.
	paceMaxBackoff = 6 * time.Hour
)

// paceKey is one query: its connection (0 = none) and cache key, which
// holds the login and the parameters. A quota shared by several queries
// shows in each one's Remaining.
type paceKey struct {
	conn  int64
	query string
	main  bool // the connection's dataset, not a detail or extra query
}

// paceRule is a query's interval before the service has its say.
type paceRule struct {
	base   time.Duration
	manual bool
}

type pacer struct {
	mu     sync.Mutex
	states map[paceKey]Pace
}

var pace = &pacer{states: map[paceKey]Pace{}}

// next records a fetch and returns when the query may run again.
func (p *pacer) next(k paceKey, rule paceRule, u sources.Usage, outcome fetchOutcome, now time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	prev := p.states[k]
	state := Pace{Every: rule.base, Reason: PaceDefault}
	if rule.manual {
		state.Reason = PaceManual
	}

	switch {
	case u.Limited:
		wait := u.RetryAt.Sub(now)
		if wait <= 0 {
			wait = min(max(2*prev.Every, rule.base), paceMaxBackoff)
		}
		state = Pace{Every: wait, Until: now.Add(wait), Reason: PaceRefused}
		p.states[k] = state
		return state.Until

	case !rule.manual:
		if every, ok := spread(u, now); ok && every > rule.base {
			state.Every, state.Reason = every, PaceTight
		}
	}

	p.states[k] = state
	if outcome == fetchFailed {
		return now.Add(min(state.Every, errorTTL))
	}
	return now.Add(state.Every)
}

// spread is the interval at which the quota left above the reserve lasts
// until it renews; ok is false when the service told no quota.
func spread(u sources.Usage, now time.Time) (time.Duration, bool) {
	left := u.Reset.Sub(now)
	if u.Limit <= 0 || u.Calls <= 0 || left <= 0 {
		return 0, false
	}

	spendable := float64(u.Remaining) - float64(u.Limit)*paceReserve
	if spendable < float64(u.Calls) {
		return left, true
	}
	return time.Duration(float64(left) * float64(u.Calls) / spendable), true
}

// paceRank orders reasons by how much they hold a query back.
var paceRank = map[PaceReason]int{PaceNone: 0, PaceDefault: 1, PaceManual: 1, PaceTight: 2, PaceRefused: 3}

// of is a connection's pace as its record shows it: a refused or
// stretched query first, else its main query.
func (p *pacer) of(connID int64) Pace {
	p.mu.Lock()
	defer p.mu.Unlock()

	var out Pace
	for k, s := range p.states {
		if k.conn != connID || (!k.main && paceRank[s.Reason] < paceRank[PaceTight]) {
			continue
		}
		if paceRank[s.Reason] > paceRank[out.Reason] || (paceRank[s.Reason] == paceRank[out.Reason] && s.Every > out.Every) {
			out = s
		}
	}
	return out
}

// PaceOf is how a connection's queries are paced now; Reason is PaceNone
// before its first fetch since the start.
func PaceOf(connID int64) Pace { return pace.of(connID) }

// isMain tells a connection's dataset query from its other ones.
func isMain(source sources.Source, conn *model.Connection) bool {
	return conn != nil && source.Key() == sources.DataKey(enums.ServiceType(conn.Service))
}

// ruleOf is a query's interval: the connection's own for its main query,
// else the source's.
func ruleOf(source sources.Source, conn *model.Connection) paceRule {
	if isMain(source, conn) && conn.RefreshMinutes > 0 {
		return paceRule{base: time.Duration(conn.RefreshMinutes) * time.Minute, manual: true}
	}
	return paceRule{base: source.TTL()}
}

// Minutes is the interval in whole minutes, at least 1.
func (p Pace) Minutes() int { return max(int(p.Every.Minutes()), 1) }

// DefaultMinutes is a service's main query interval when automatic.
func DefaultMinutes(service enums.ServiceType) int {
	source, err := sources.Get(sources.DataKey(service))
	if err != nil {
		return 0
	}
	return max(int(source.TTL().Minutes()), 1)
}
