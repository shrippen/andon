// Package hooks receives events that services push (PG Back Web
// webhooks) or their whole state (Hansei), and hands out the URL they
// must call.
//
//	POST /hooks/{connection id}/{HMAC(master key, id + nonce)}  body {"event", "name"}
//	     → hook_events row → pgbackweb.data (svcdata passes the events)
//	POST /hooks/{connection id}/{…}                            body {"state": {…}}
//	     → replaces the connection's state → hansei.data (svcdata passes it)
package hooks

import (
	"andon/internal/caps"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	"andon/internal/repos/misc"
	"andon/internal/services/svcdata"
)

const (
	subjectMax = 120
	eventMax   = 60
	keepDays   = 60
	pathPrefix = "/hooks/"
)

// ErrRejected hides whether the connection or the signature was wrong.
var ErrRejected = errors.New("hooks: rejected")

// ErrThrottled means a URL sent more than perMinute events.
var ErrThrottled = errors.New("hooks: throttled")

// perMinute caps the events one connection accepts; PG Back Web sends a
// handful per backup run, a leaked URL could send millions.
const perMinute = 60

// nonceKey stores a connection's nonce (settings table); rotating it
// changes the URL and revokes the old one. No nonce: URLs from before.
const nonceKey = "hook."

// Accepts reports whether a service is fed by webhooks (caps.Traits).
func Accepts(service enums.ServiceType) bool { return caps.TraitsOf(service).Webhooks }

func signature(q db.Queryer, connID int64) (string, error) {
	id := strconv.FormatInt(connID, 10)
	raw, err := misc.Setting(q, nonceKey+id)
	if err != nil {
		return "", err
	}
	message := "hook:" + id
	if nonce, _ := raw["nonce"].(string); nonce != "" {
		message += ":" + nonce
	}
	return crypto.Sign(message, crypto.PurposeHook)
}

// Rotate gives a connection a new webhook URL; the old one stops working.
// The caller checks the right to manage the connection.
func Rotate(q db.Queryer, connID int64) error {
	nonce := crypto.NewToken()
	return misc.SetSetting(q, nonceKey+strconv.FormatInt(connID, 10), map[string]any{"nonce": nonce})
}

// URL is the webhook address for a connection, e.g.
// https://dash.example/hooks/7/q3…
func URL(q db.Queryer, baseURL string, connID int64) (string, error) {
	sig, err := signature(q, connID)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(baseURL, "/") + pathPrefix + strconv.FormatInt(connID, 10) + "/" + sig, nil
}

// Receive stores one event after checking the URL's signature.
func Receive(d *sql.DB, connID int64, sig, event, subject string) error {
	want, err := signature(d, connID)
	if err != nil || !crypto.Same(want, sig) {
		return ErrRejected
	}
	if !allow(connID, time.Now()) {
		return ErrThrottled
	}
	event, subject = clip(strings.TrimSpace(event), eventMax), clip(strings.TrimSpace(subject), subjectMax)
	if event == "" {
		return ErrRejected
	}

	err = db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil || !Accepts(enums.ServiceType(conn.Service)) {
			return ErrRejected
		}
		now := time.Now().UTC()
		if err := data.PruneHookEvents(tx, now.AddDate(0, 0, -keepDays)); err != nil {
			return err
		}
		return data.AddHookEvent(tx, &model.HookEvent{ConnectionID: connID, Event: event, Subject: subject, At: now})
	})
	if err != nil {
		return err
	}
	svcdata.Forget(connID)
	return nil
}

// ReceiveState replaces a connection's pushed state after checking the
// URL's signature; each push counts like one event against the cap.
func ReceiveState(d *sql.DB, connID int64, sig string, state map[string]any) error {
	want, err := signature(d, connID)
	if err != nil || !crypto.Same(want, sig) {
		return ErrRejected
	}
	if !allow(connID, time.Now()) {
		return ErrThrottled
	}
	err = db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil || !Accepts(enums.ServiceType(conn.Service)) {
			return ErrRejected
		}
		return data.SetHookState(tx, connID, state, time.Now().UTC())
	})
	if err != nil {
		return err
	}
	svcdata.Forget(connID)
	return nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var (
	floodMu sync.Mutex
	flood   = map[int64][]time.Time{} // accepted events of the last minute
)

// allow counts an event against the connection's per-minute cap.
func allow(connID int64, now time.Time) bool {
	floodMu.Lock()
	defer floodMu.Unlock()

	recent := flood[connID][:0]
	for _, at := range flood[connID] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= perMinute {
		flood[connID] = recent
		return false
	}
	flood[connID] = append(recent, now)
	return true
}
