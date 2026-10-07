// Package audit records who changed what, from where.
package audit

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"andon/internal/db"
	"andon/internal/model"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
)

// Retention is how long audit entries are kept before Prune removes them.
const Retention = 365 * 24 * time.Hour

// ErrDenied is returned by Entries for a non-admin principal.
var ErrDenied = errors.New("audit: admin only")

// Log records one action inside the caller's transaction. detail must never
// hold secrets: it is stored as-is and shown in the admin audit log.
func Log(q db.Queryer, userID *int64, action, target, ip string, detail map[string]any) error {
	return misc.Audit(q, &model.AuditEntry{
		At: time.Now().UTC(), UserID: userID, Action: action, Target: target, IP: ip, Detail: detail,
	})
}

// Changes lists the fields whose value changes as field → [old, new], e.g.
// {"open": [false, true]}. Values are compared in their stored JSON form,
// so ["a"] and []any{"a"} are equal.
func Changes(stored, value map[string]any) map[string]any {
	changes := map[string]any{}
	for k, v := range normalize(value) {
		before, set := stored[k]
		if reflect.DeepEqual(before, v) || (!set && isZero(v)) {
			continue
		}
		changes[k] = []any{stored[k], v}
	}
	return changes
}

// isZero: false, "", 0 or nothing, the same as a value never set.
func isZero(v any) bool {
	return v == nil || v == false || v == "" || v == 0.0
}

// normalize converts a value to what reading it back from JSON yields.
func normalize(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return value
	}
	return out
}

// Entries returns the most recent page of audit entries. Admin only.
func Entries(q db.Queryer, who *access.Principal) ([]*model.AuditEntry, error) {
	if !who.IsAdmin() {
		return nil, ErrDenied
	}
	return misc.AuditPage(q, nil)
}

// Prune deletes audit entries older than Retention.
func Prune(q db.Queryer) error {
	return misc.PruneAudit(q, time.Now().UTC().Add(-Retention))
}
