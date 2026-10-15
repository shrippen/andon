// Package journal lists what Andon wrote to other services for the user
// (from the audit log) and takes back what a service lets it take back:
//
//	audit_log ──Entries──► the page, newest first, per area
//	          ──Undo─────► vatcheck.skip → vatcheck.Restore (back in the feed)
//	                       receipts.link → receipts.Unlink (both sides)
//	                       euer.set      → euer.Unset (field empty again)
//
// An undo is a write of its own and shows as one.
package journal

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/model"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/euer"
	"andon/internal/services/receipts"
	"andon/internal/services/vatcheck"
)

// Area groups writes by the page that made them.
type Area string

const (
	AreaAll      Area = ""
	AreaVAT      Area = "vat"
	AreaEuer     Area = "euer"
	AreaReceipts Area = "receipts"
	AreaBilling  Area = "billing"
	AreaKimai    Area = "kimai"
	AreaSubs     Area = "subs"
	AreaBank     Area = "bank"
	AreaHome     Area = "home"
	AreaHomelab  Area = "homelab"
)

// Areas lists them in the page's order.
var Areas = []Area{AreaVAT, AreaEuer, AreaReceipts, AreaBilling, AreaKimai, AreaSubs, AreaBank, AreaHomelab, AreaHome}

// kinds are the audit actions that wrote to another service, by prefix.
var kinds = []struct {
	prefix string
	area   Area
}{
	{"vatcheck.", AreaVAT}, {"vendors.", AreaVAT}, {"euer.", AreaEuer}, {"receipts.", AreaReceipts}, {"mail.to_paperless", AreaReceipts},
	{"billing.draft", AreaBilling}, {"billing.payment", AreaBilling}, {"dunning.", AreaBilling},
	{"kimai.", AreaKimai}, {"week.", AreaKimai}, {"travel.", AreaKimai},
	{"subs.", AreaSubs}, {"categorize.", AreaBank}, {"updates.", AreaHomelab}, {"inventory.", AreaHomelab},
	{"hass.", AreaHome}, {"dns.", AreaHome}, {"grocy.", AreaHome}, {"tandoor.", AreaHome},
}

// shown is how many entries the page lists.
const shown = 300

// Undoable actions.
const (
	actArchived = "vatcheck.skip"
	actLinked   = "receipts.link"
	actEuer     = "euer.set"
)

var (
	// ErrNotFound: no such entry of the caller's.
	ErrNotFound = errors.New("journal.not_found")
	// ErrNoUndo: the entry cannot be taken back, or lacks what it needs.
	ErrNoUndo = errors.New("journal.no_undo")
)

// Entry is one write.
type Entry struct {
	ID     int64
	At     time.Time
	Action string // the audit action: "vatcheck.link"
	Area   Area
	Target string // what it wrote to: an expense number, a payee
	Detail map[string]any
	Undo   bool // the service lets it be taken back
}

// Label is the action's i18n key: journal.act_vatcheck_link.
func (e Entry) Label() string {
	return "journal.act_" + strings.NewReplacer(".", "_").Replace(e.Action)
}

// Entries lists the caller's writes of area (AreaAll: every one).
func Entries(d *sql.DB, who *access.Principal, area Area) ([]Entry, error) {
	var prefixes []string
	for _, k := range kinds {
		if area == AreaAll || k.area == area {
			prefixes = append(prefixes, k.prefix)
		}
	}
	rows, err := misc.AuditOf(d, who.UserID, prefixes, shown)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, entryOf(r))
	}
	return out, nil
}

func entryOf(r *model.AuditEntry) Entry {
	e := Entry{ID: r.ID, At: r.At, Action: r.Action, Target: r.Target, Detail: r.Detail}
	for _, k := range kinds {
		if strings.HasPrefix(r.Action, k.prefix) {
			e.Area = k.area
			break
		}
	}
	switch r.Action {
	case actArchived:
		e.Undo = str(r.Detail, "txn") != ""
	case actLinked:
		e.Undo = str(r.Detail, "expense") != "" && len(ids(str(r.Detail, "docs"))) > 0
	case actEuer:
		e.Undo = str(r.Detail, "expense") != "" && str(r.Detail, "slot") != ""
	}
	return e
}

// Undo takes one of the caller's writes back.
func Undo(ctx context.Context, d *sql.DB, who *access.Principal, id int64, ip string) error {
	r, err := misc.AuditEntry(d, id)
	if err != nil {
		return err
	}
	if r == nil || r.UserID == nil || *r.UserID != who.UserID {
		return ErrNotFound
	}
	e := entryOf(r)
	if !e.Undo {
		return ErrNoUndo
	}
	switch e.Action {
	case actArchived:
		return vatcheck.Restore(ctx, d, who, str(e.Detail, "scope"), str(e.Detail, "txn"), ip)
	case actLinked:
		for _, doc := range ids(str(e.Detail, "docs")) {
			if err := receipts.Unlink(ctx, d, who, str(e.Detail, "expense"), doc, ip); err != nil {
				return err
			}
		}
		return nil
	case actEuer:
		slot, _ := strconv.Atoi(str(e.Detail, "slot"))
		return euer.Unset(ctx, d, who, str(e.Detail, "scope"), str(e.Detail, "expense"), slot, ip)
	}
	return ErrNoUndo
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// ids reads "12,13".
func ids(list string) []int64 {
	var out []int64
	for _, part := range strings.Split(list, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
