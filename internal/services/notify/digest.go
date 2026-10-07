package notify

// The digest mail as the user sets it up: when (time, weekday), what
// (from which level, deadlines, empty digests), a preview, a test send
// and the last sends.
//
//	prefs["digest"]     {"daily": "07:30", "weekly": "mon", "min": 20, "deadlines": "off", "empty": "skip", "no_summary": true}
//	prefs["digest_log"] [{"at": "2026-10-07T07:30:00Z", "kind": "auto", "state": "sent", "rows": 4}, …] newest first

import (
	"database/sql"
	"errors"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/outbound"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/mail"
)

// DeadlineUse says whether the digest lists tax deadlines.
type DeadlineUse string

const (
	DeadlinesOn  DeadlineUse = "" // default: listed
	DeadlinesOff DeadlineUse = "off"
)

// EmptyUse says whether a digest without entries goes out.
type EmptyUse string

const (
	EmptySend EmptyUse = "" // default: "nothing open" is news too
	EmptySkip EmptyUse = "skip"
)

// DigestKind is what started a send.
type DigestKind string

const (
	DigestByTime DigestKind = "auto"
	DigestByTest DigestKind = "test"
)

// DigestState is how a send ended.
type DigestState string

const (
	DigestSent   DigestState = "sent"
	DigestFailed DigestState = "failed"
	DigestEmpty  DigestState = "empty" // nothing to say, left at home
)

// ErrNoMail: no SMTP server is set, mails cannot go out.
var ErrNoMail = errors.New("notify.no_mail")

const (
	digestLogKey = "digest_log"
	digestLogMax = 10
	minKey       = "min"
	deadlinesKey = "deadlines"
	emptyKey     = "empty"
)

// DigestEntry is one send in the log.
type DigestEntry struct {
	At    time.Time
	Kind  DigestKind
	State DigestState
	Rows  int
}

// Digest is a user's digest mail setup and its recent sends.
type Digest struct {
	Daily     string // time "HH:MM", "" = no digest
	Weekly    string // weekday key ("mon".."sun"), "" = every day
	MinLevel  enums.Severity
	Deadlines DeadlineUse
	Empty     EmptyUse
	NoSummary bool          // opt-out of the LLM summary in the weekly digest
	Log       []DigestEntry // newest first, read only
}

// digestOf reads the setup from a user's prefs.
func digestOf(prefs map[string]any) Digest {
	raw, _ := prefs[digestKey].(map[string]any)
	var g Digest
	g.Daily, _ = raw["daily"].(string)
	g.Weekly, _ = raw["weekly"].(string)
	g.NoSummary, _ = raw[noSummaryKey].(bool)
	if level, ok := raw[minKey].(float64); ok {
		g.MinLevel = enums.Severity(level)
	}
	deadlines, _ := raw[deadlinesKey].(string)
	empty, _ := raw[emptyKey].(string)
	g.Deadlines, g.Empty = DeadlineUse(deadlines), EmptyUse(empty)

	list, _ := prefs[digestLogKey].([]any)
	for _, item := range list {
		e, _ := item.(map[string]any)
		at, _ := e["at"].(string)
		kind, _ := e["kind"].(string)
		state, _ := e["state"].(string)
		rows, _ := e["rows"].(float64)
		when, _ := time.Parse(time.RFC3339, at)
		g.Log = append(g.Log, DigestEntry{At: when, Kind: DigestKind(kind), State: DigestState(state), Rows: int(rows)})
	}
	return g
}

// GetDigest reads the caller's digest setup and log.
func GetDigest(d *sql.DB, who *access.Principal) (Digest, error) {
	var out Digest
	err := db.WithRead(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil || u == nil {
			return orNotFound(err)
		}
		out = digestOf(u.Prefs)
		return nil
	})
	return out, err
}

// SaveDigest writes the caller's digest setup; the log stays.
func SaveDigest(d *sql.DB, who *access.Principal, g Digest) error {
	if g.Daily != "" && parseClock(g.Daily) == nil {
		return ErrBadTime
	}
	if g.Weekly != "" && weekdayIndex(g.Weekly) < 0 {
		return ErrBadTime
	}
	return changePrefs(d, who.UserID, func(prefs map[string]any) {
		prefs[digestKey] = map[string]any{"daily": g.Daily, "weekly": g.Weekly, noSummaryKey: g.NoSummary,
			minKey: float64(g.MinLevel), deadlinesKey: string(g.Deadlines), emptyKey: string(g.Empty)}
	})
}

// changePrefs edits a copy of a user's prefs and stores it.
func changePrefs(d *sql.DB, userID int64, edit func(map[string]any)) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, userID)
		if err != nil || u == nil {
			return orNotFound(err)
		}
		prefs := map[string]any{}
		for k, v := range u.Prefs {
			prefs[k] = v
		}
		edit(prefs)
		u.Prefs = prefs
		return users.Update(tx, u)
	})
}

// logDigest puts a send on top of the user's log, keeping digestLogMax.
func logDigest(d *sql.DB, userID int64, e DigestEntry) error {
	return changePrefs(d, userID, func(prefs map[string]any) {
		old, _ := prefs[digestLogKey].([]any)
		entry := map[string]any{"at": e.At.UTC().Format(time.RFC3339), "kind": string(e.Kind), "state": string(e.State), "rows": float64(e.Rows)}
		list := append([]any{entry}, old...)
		prefs[digestLogKey] = list[:min(len(list), digestLogMax)]
	})
}

// DigestPreview renders the caller's digest as it would go out now,
// without the LLM summary (it costs and takes minutes), sending nothing.
func DigestPreview(d *sql.DB, who *access.Principal) (outbound.Mail, error) {
	prefs, err := prefsOf(d, who.UserID)
	if err != nil {
		return outbound.Mail{}, err
	}
	m, _, err := composeDigest(d, who, prefs, time.Now().In(berlin), summaryOff)
	return m, err
}

// DigestTest sends the caller's digest now to their own address and logs
// it as a test; the daily send stays due.
func DigestTest(d *sql.DB, who *access.Principal) error {
	if !mail.Configured() {
		return ErrNoMail
	}
	prefs, err := prefsOf(d, who.UserID)
	if err != nil {
		return err
	}
	m, rows, err := composeDigest(d, who, prefs, time.Now().In(berlin), summaryOff)
	if err != nil {
		return err
	}

	entry := DigestEntry{At: time.Now(), Kind: DigestByTest, State: DigestSent, Rows: rows}
	sendErr := mail.SendNow(m)
	if sendErr != nil {
		entry.State = DigestFailed
	}
	if err := logDigest(d, who.UserID, entry); err != nil {
		return err
	}
	if sendErr != nil {
		return ErrFailed
	}
	return nil
}

// prefsOf reads a user's prefs.
func prefsOf(d *sql.DB, userID int64) (map[string]any, error) {
	var prefs map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, userID)
		if err != nil || u == nil {
			return orNotFound(err)
		}
		prefs = u.Prefs
		return nil
	})
	return prefs, err
}

// summaryUse says whether a digest asks the LLM for its weekly prose.
type summaryUse int

const (
	summaryOn summaryUse = iota
	summaryOff
)
