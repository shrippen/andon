package httpclient

// Usage of the calls made under one context: how many, and what the
// services said about their rate limits. The fetch pace reads it:
//
//	ctx, m := Metered(ctx)
//	fetch(ctx)        every Request notes its answer
//	m.Usage()      →  {Calls: 4, Limit: 5000, Remaining: 12, Reset: 14:05}
//
// Headers read (GitHub, Gitea, most REST APIs and the IETF draft):
//
//	X-RateLimit-Limit / RateLimit-Limit          quota
//	X-RateLimit-Remaining / RateLimit-Remaining  left of it
//	X-RateLimit-Reset / RateLimit-Reset          renewal: Unix time or seconds from now
//	Retry-After                                  seconds or an HTTP date
//	429, or 403 with nothing remaining           refused for its rate

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Usage is what a fetch's calls cost and what the service allows.
type Usage struct {
	Calls     int
	Limit     int       // the quota, 0 = not told
	Remaining int       // left of it while Limit > 0
	Reset     time.Time // when the quota renews, zero = not told
	RetryAt   time.Time // when a refused call may be retried, zero = not told
	Limited   bool      // a call was refused for its rate
}

// Meter collects the Usage of one context's calls.
type Meter struct {
	mu sync.Mutex
	u  Usage
}

type meterKey struct{}

// Metered returns a context whose calls a new Meter counts.
func Metered(ctx context.Context) (context.Context, *Meter) {
	m := &Meter{}
	return context.WithValue(ctx, meterKey{}, m), m
}

// Usage is what the meter has seen so far.
func (m *Meter) Usage() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.u
}

// unixFloor tells a Unix reset time (GitHub) from seconds from now
// (IETF): no quota renews more than 30 years ahead.
const unixFloor = 1e9

// noteUsage adds one answer to the context's meter, if any.
func noteUsage(ctx context.Context, resp *http.Response, now time.Time) {
	m, ok := ctx.Value(meterKey{}).(*Meter)
	if !ok {
		return
	}

	h := resp.Header
	limit, hasLimit := headerInt(h, "X-RateLimit-Limit", "RateLimit-Limit")
	remaining, hasRemaining := headerInt(h, "X-RateLimit-Remaining", "RateLimit-Remaining")
	var reset time.Time
	if v, ok := headerInt(h, "X-RateLimit-Reset", "RateLimit-Reset"); ok {
		reset = now.Add(time.Duration(v) * time.Second)
		if v > unixFloor {
			reset = time.Unix(int64(v), 0)
		}
	}
	refused := resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && hasRemaining && remaining == 0)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.u.Calls++

	// Several quotas (GitHub's search has its own): the tightest counts.
	if hasLimit && limit > 0 && hasRemaining && (m.u.Limit == 0 || remaining*m.u.Limit < m.u.Remaining*limit) {
		m.u.Limit, m.u.Remaining, m.u.Reset = limit, remaining, reset
	}
	if !refused {
		return
	}
	m.u.Limited = true
	if at := retryAt(h.Get("Retry-After"), now); at.After(m.u.RetryAt) {
		m.u.RetryAt = at
	}
	if m.u.RetryAt.IsZero() && reset.After(now) {
		m.u.RetryAt = reset
	}
}

// headerInt reads the first number of the first header present:
// "100" and "100, 100;w=3600" are both 100.
func headerInt(h http.Header, names ...string) (int, bool) {
	for _, name := range names {
		raw := strings.TrimSpace(h.Get(name))
		if raw == "" {
			continue
		}
		if i := strings.IndexAny(raw, ",;"); i >= 0 {
			raw = raw[:i]
		}
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		return n, err == nil
	}
	return 0, false
}

// retryAt reads Retry-After: seconds or an HTTP date; zero if absent.
func retryAt(raw string, now time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if s, err := strconv.Atoi(raw); err == nil {
		return now.Add(time.Duration(s) * time.Second)
	}
	at, err := http.ParseTime(raw)
	if err != nil {
		return time.Time{}
	}
	return at
}
