// Package hints stores rule findings, reconciles them per run, and lets
// users act on them.
//
//	rule run (space, user?, connection)
//	    findings ──► upsert by fingerprint ──► missing ones: resolved
//	                                            reappearing: reopened, marks dropped
//	reader
//	    open hints of reachable spaces − acknowledged − snoozed(until > now)
package hints

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/spaces"
)

const (
	resolvedRetention = 90 * 24 * time.Hour
	ackModeKey        = "hint_ack"
)

// ErrNotFound means the hint does not exist.
var ErrNotFound = errors.New("hints: not found")

// ErrDenied means the principal may not act on this hint.
var ErrDenied = errors.New("hints: access denied")

// Sync applies one rule run: upserts findings by fingerprint, reopens
// previously resolved hints that reappeared (dropping their old marks),
// and resolves hints of these rules that didn't fire this time. Returns
// the number of hints that are new or reopened.
func Sync(q db.Queryer, spaceID int64, userID *int64, connID *int64, ruleIDs []string, findings []rules.Finding) (int, error) {
	now := time.Now().UTC()
	seen := map[string]bool{}
	fresh := 0

	for _, item := range findings {
		// Two connections of one service in a space must not share fingerprints.
		fingerprint := item.Fingerprint
		if connID != nil {
			fingerprint = fmt.Sprintf("%d:%s", *connID, item.Fingerprint)
		}
		seen[fingerprint] = true

		hint, err := data.HintByPrint(q, spaceID, userID, fingerprint)
		if err != nil {
			return 0, err
		}
		if hint == nil {
			hint = &model.Hint{SpaceID: spaceID, UserID: userID, Fingerprint: fingerprint, FirstSeen: now}
			fill(hint, item, connID, now)
			if err := data.AddHint(q, hint); err != nil {
				return 0, err
			}
			if err := logEvent(q, hint.ID, enums.EventOpened, nil, ""); err != nil {
				return 0, err
			}
			fresh++
			continue
		}

		if hint.ResolvedAt != nil {
			hint.ResolvedAt = nil
			hint.FirstSeen = now
			if err := data.DropMarks(q, hint.ID); err != nil {
				return 0, err
			}
			if err := logEvent(q, hint.ID, enums.EventReopened, nil, ""); err != nil {
				return 0, err
			}
			fresh++
		}
		wasCritical := hint.Severity == enums.SeverityCritical
		fill(hint, item, connID, now)
		if overdue(hint, item, now) {
			hint.Severity = enums.SeverityCritical
			if !wasCritical {
				if err := logEvent(q, hint.ID, enums.EventEscalated, nil, ""); err != nil {
					return 0, err
				}
			}
		}
		if err := data.UpdateHint(q, hint); err != nil {
			return 0, err
		}
	}

	stillFiring, err := data.HintsOfScope(q, spaceID, userID, ruleIDs, connID)
	if err != nil {
		return 0, err
	}
	for _, h := range stillFiring {
		if !seen[h.Fingerprint] && h.ResolvedAt == nil {
			h.ResolvedAt = &now
			if err := data.UpdateHint(q, h); err != nil {
				return 0, err
			}
			if err := logEvent(q, h.ID, enums.EventResolved, nil, ""); err != nil {
				return 0, err
			}
		}
	}

	return fresh, nil
}

// overdue: the rule escalates and the hint has been open long enough.
func overdue(hint *model.Hint, item rules.Finding, now time.Time) bool {
	return item.EscalateDays > 0 && now.Sub(hint.FirstSeen) >= time.Duration(item.EscalateDays)*24*time.Hour
}

func fill(hint *model.Hint, item rules.Finding, connID *int64, now time.Time) {
	hint.Rule = item.Rule
	hint.Severity = item.Severity
	hint.Message = item.Message
	hint.Params = item.Params
	hint.ActionURL = item.ActionURL
	hint.ActionLabel = item.ActionLabel
	hint.Due = item.Due
	hint.Sources = item.Sources
	hint.ConnectionID = connID
	hint.LastSeen = now
}

// ── Read ──

// View is a hint rendered for one reader: translated title/why, formatted
// params, in the reader's own language.
type View struct {
	ID           int64
	Rule         string
	Severity     enums.Severity
	Title        string
	Why          string
	ActionURL    string
	ActionLabel  string
	Due          string
	Sources      []string
	SpaceName    string
	FirstSeen    time.Time
	ConnectionID *int64
	Assignee     string // "" = nobody
	AssigneeID   *int64
	Work         enums.WorkState
	Flapping     bool     // reopened often lately; pushed only once
	Client       string   // the client or customer the hint names, "" if none
	Items        []string // the machines the hint names (host, node, guest, device), see itemParams
	Value        float64  // largest money amount the hint names, 0 if none
	Currency     string
	Maintenance  bool       // in a planned work window of its space: not pushed
	ResolvedAt   *time.Time // set in the done list
	// Handled is how the user put the hint aside (done or paused), with
	// when; set in the done list (Handled).
	Handled   enums.HintState
	HandledAt *time.Time
}

func hidden(marks []*model.HintMark, now time.Time) bool {
	for _, m := range marks {
		if m.State == enums.HintAcknowledged {
			return true
		}
		if m.State == enums.HintSnoozed && m.Until != nil && m.Until.After(now) {
			return true
		}
	}
	return false
}

func visible(q db.Queryer, who *access.Principal) ([]*model.Hint, error) {
	spaceIDs := make([]int64, 0, len(who.Spaces))
	for id := range who.Spaces {
		spaceIDs = append(spaceIDs, id)
	}
	found, err := data.HintsIn(q, spaceIDs, who.UserID)
	if err != nil {
		return nil, err
	}

	hintIDs := make([]int64, len(found))
	for i, h := range found {
		hintIDs[i] = h.ID
	}
	marks, err := data.Marks(q, hintIDs, who.UserID)
	if err != nil {
		return nil, err
	}
	byHint := map[int64][]*model.HintMark{}
	for _, m := range marks {
		byHint[m.HintID] = append(byHint[m.HintID], m)
	}

	now := time.Now().UTC()
	out := found[:0]
	for _, h := range found {
		if !hidden(byHint[h.ID], now) {
			out = append(out, h)
		}
	}
	return out, nil
}

// Active returns the hints visible to who, filtered to min severity and
// (if given) at least one matching source, most severe / soonest due /
// oldest first, capped at limit if positive.
func Active(d *sql.DB, who *access.Principal, minSeverity enums.Severity, sourcesFilter []string, limit int) ([]View, error) {
	return Filtered(d, who, Filter{MinSeverity: minSeverity, Sources: sourcesFilter}, limit)
}

// Resolved lists the hints whose condition went away since a time,
// newest first: the "done" view of the hints page.
func Resolved(d *sql.DB, who *access.Principal, since time.Time) ([]View, error) {
	var views []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		found, err := data.ResolvedHintsIn(tx, spaceIDs, who.UserID, since.UTC())
		if err != nil {
			return err
		}
		for _, h := range found {
			v := viewOf(h, who)
			v.ResolvedAt = h.ResolvedAt
			views = append(views, v)
		}
		return nil
	})
	return views, err
}

// Handled returns the open hints who marked done or paused since then,
// newest first, so the done list can bring them back.
func Handled(d *sql.DB, who *access.Principal, since time.Time) ([]View, error) {
	var views []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		found, err := data.HintsIn(tx, spaceIDs, who.UserID)
		if err != nil {
			return err
		}
		ids := make([]int64, len(found))
		for i, h := range found {
			ids[i] = h.ID
		}
		marks, err := data.Marks(tx, ids, who.UserID)
		if err != nil {
			return err
		}
		byHint := map[int64]*model.HintMark{}
		for _, m := range marks {
			byHint[m.HintID] = m
		}
		now := time.Now().UTC()
		for _, h := range found {
			m := byHint[h.ID]
			if m == nil || m.At.Before(since) || !hidden([]*model.HintMark{m}, now) {
				continue
			}
			v := viewOf(h, who)
			at := m.At
			v.Handled, v.HandledAt = m.State, &at
			views = append(views, v)
		}
		return nil
	})
	sort.SliceStable(views, func(i, j int) bool { return views[i].HandledAt.After(*views[j].HandledAt) })
	return views, err
}

// Filter narrows the visible hints; empty lists match everything.
type Filter struct {
	MinSeverity enums.Severity
	Sources     []string
	Rules       []string
}

// Filtered is Active with a rule filter too (topic widgets).
func Filtered(d *sql.DB, who *access.Principal, f Filter, limit int) ([]View, error) {
	var views []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		found, err := visible(tx, who)
		if err != nil {
			return err
		}

		rows := found[:0]
		for _, h := range found {
			if h.Severity < f.MinSeverity {
				continue
			}
			if len(f.Sources) > 0 && !anyMatch(h.Sources, f.Sources) {
				continue
			}
			if len(f.Rules) > 0 && !anyMatch([]string{h.Rule}, f.Rules) {
				continue
			}
			rows = append(rows, h)
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Severity != rows[j].Severity {
				return rows[i].Severity > rows[j].Severity
			}
			di, dj := rows[i].Due, rows[j].Due
			if di == "" {
				di = "9999"
			}
			if dj == "" {
				dj = "9999"
			}
			if di != dj {
				return di < dj
			}
			return rows[i].FirstSeen.Before(rows[j].FirstSeen)
		})
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}

		windows, err := maintenanceOf(tx, rows)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		views = make([]View, len(rows))
		for i, h := range rows {
			views[i] = viewOf(h, who)
			views[i].Maintenance = windows[h.SpaceID].Covers(h.ConnectionID, now)
		}
		return enrich(tx, views)
	})
	return views, err
}

// maintenanceOf reads the work window of every space the hints belong to.
func maintenanceOf(q db.Queryer, rows []*model.Hint) (map[int64]spaces.Maintenance, error) {
	out := map[int64]spaces.Maintenance{}
	for _, h := range rows {
		if _, done := out[h.SpaceID]; done {
			continue
		}
		sp, err := content.Space(q, h.SpaceID)
		if err != nil {
			return nil, err
		}
		if sp != nil {
			out[h.SpaceID] = spaces.MaintenanceOf(sp.Settings)
		}
	}
	return out, nil
}

func anyMatch(have, want []string) bool {
	set := map[string]bool{}
	for _, w := range want {
		set[w] = true
	}
	for _, h := range have {
		if set[h] {
			return true
		}
	}
	return false
}

func viewOf(h *model.Hint, who *access.Principal) View {
	locale := who.Locale
	params := i18n.Typed(h.Params, locale)
	spaceName := ""
	if sp, ok := who.Spaces[h.SpaceID]; ok {
		spaceName = sp.Name
	}
	actionLabel := ""
	if h.ActionLabel != "" {
		actionLabel = i18n.T("action."+h.ActionLabel, locale, nil)
	}
	value, currency := moneyValue(h.Params)
	return View{
		ID: h.ID, Rule: h.Rule, Severity: h.Severity, Value: value, Currency: currency, Client: clientOf(h.Params),
		Items:     itemsOf(h.Params),
		Title:     i18n.T("hint."+h.Message+".title", locale, params),
		Why:       i18n.T("hint."+h.Message+".why", locale, params),
		ActionURL: h.ActionURL, ActionLabel: actionLabel, Due: h.Due, Sources: h.Sources,
		SpaceName: spaceName, FirstSeen: h.FirstSeen, ConnectionID: h.ConnectionID,
	}
}

// clientParams are the params a hint names its client by.
var clientParams = []string{"client", "customer"}

// clientOf is the client a hint's params name, "" if none.
func clientOf(params map[string]any) string {
	for _, k := range clientParams {
		if name, ok := params[k].(string); ok && name != "" && name != "?" {
			return name
		}
	}
	return ""
}

// itemParams are the params a hint names a machine by: a host, a
// Proxmox node or guest, a device, a Prometheus instance, a proxy target.
var itemParams = []string{"host", "node", "guest", "vm", "device", "instance", "target"}

// itemsOf lists the machines a hint's params name, in itemParams order.
func itemsOf(params map[string]any) []string {
	var out []string
	for _, k := range itemParams {
		if name, ok := params[k].(string); ok && name != "" && name != "?" && name != "–" {
			out = append(out, name)
		}
	}
	return out
}

// CountFor returns (count, highest severity) of open hints on one connection.
func CountFor(d *sql.DB, who *access.Principal, connID int64) (int, enums.Severity, error) {
	all, err := Badges(d, who)
	badge := all[connID]
	return badge.Count, badge.Top, err
}

// Badge is a connection's open hints: how many, the most severe.
type Badge struct {
	Count int
	Top   enums.Severity
}

// Badges returns the badge of every connection with open hints, in one
// read: a board shows one per tile that has a connection.
func Badges(d *sql.DB, who *access.Principal) (map[int64]Badge, error) {
	out := map[int64]Badge{}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		found, err := visible(tx, who)
		if err != nil {
			return err
		}
		for _, h := range found {
			if h.ConnectionID == nil {
				continue
			}
			badge := out[*h.ConnectionID]
			badge.Count++
			badge.Top = max(badge.Top, h.Severity)
			out[*h.ConnectionID] = badge
		}
		return nil
	})
	return out, err
}

// ForConnection lists the open hints of one connection, most severe
// first (the hint badge on a link tile).
func ForConnection(d *sql.DB, who *access.Principal, connID int64) ([]View, error) {
	all, err := Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		return nil, err
	}

	var out []View
	for _, v := range all {
		if v.ConnectionID != nil && *v.ConnectionID == connID {
			out = append(out, v)
		}
	}
	return out, nil
}

// Summary counts open hints per severity level.
func Summary(d *sql.DB, who *access.Principal) (map[enums.Severity]int, error) {
	counts := map[enums.Severity]int{enums.SeverityInfo: 0, enums.SeverityWarn: 0, enums.SeverityCritical: 0}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		found, err := visible(tx, who)
		if err != nil {
			return err
		}
		for _, h := range found {
			counts[h.Severity]++
		}
		return nil
	})
	return counts, err
}

// ── Act ──

func ackMode(q db.Queryer, spaceID int64) (enums.HintAckMode, error) {
	space, err := content.Space(q, spaceID)
	if err != nil {
		return "", err
	}
	if space == nil || space.Kind != enums.SpaceTeam {
		return enums.AckPerUser, nil
	}
	if raw, ok := space.Settings[ackModeKey].(string); ok && raw != "" {
		return enums.HintAckMode(raw), nil
	}
	return enums.AckPerUser, nil
}

// Mark sets (or clears, for HintOpen) one hint's acknowledgement state for
// who, honoring the space's ack mode (per-user vs. team-wide).
func Mark(d *sql.DB, who *access.Principal, hintID int64, state enums.HintState, until *time.Time, note string) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		hint, err := reachable(tx, who, hintID)
		if err != nil {
			return err
		}
		if err := logEvent(tx, hintID, markEvents[state], &who.UserID, note); err != nil {
			return err
		}

		mode, err := ackMode(tx, hint.SpaceID)
		if err != nil {
			return err
		}
		teamWide := mode == enums.AckTeam
		if teamWide && state == enums.HintAcknowledged {
			space, err := access.SpaceOf(tx, who, hint.SpaceID)
			if err != nil {
				return err
			}
			if access.SpaceRight(who, space) < enums.RightEdit {
				teamWide = false
			}
		}

		var owner *int64
		if !teamWide {
			owner = &who.UserID
		}
		existing, err := data.Mark(tx, hintID, owner)
		if err != nil {
			return err
		}
		if state == enums.HintOpen {
			if existing != nil {
				return data.RemoveMark(tx, existing.ID)
			}
			return nil
		}

		now := time.Now().UTC()
		if existing == nil {
			return data.SetMark(tx, &model.HintMark{HintID: hintID, UserID: owner, State: state, Until: until, At: now})
		}
		existing.State = state
		existing.Until = until
		existing.At = now
		return data.SetMark(tx, existing)
	})
}

// Action is a user-facing shortcut for Mark.
type Action string

const (
	ActionAck    Action = "ack"
	ActionSnooze Action = "snooze"
	ActionReopen Action = "reopen"
)

const defaultSnoozeDays = 7

// Act applies a named action to one hint; note explains it in the history.
func Act(d *sql.DB, who *access.Principal, hintID int64, action Action, days int, note string) error {
	switch action {
	case ActionAck:
		return Mark(d, who, hintID, enums.HintAcknowledged, nil, note)
	case ActionSnooze:
		if days < 1 {
			days = defaultSnoozeDays
		}
		until := time.Now().UTC().AddDate(0, 0, days)
		return Mark(d, who, hintID, enums.HintSnoozed, &until, note)
	default:
		return Mark(d, who, hintID, enums.HintOpen, nil, note)
	}
}

// Snooze choices of the hint detail, e.g. "until Monday".
const (
	SnoozeTomorrow = "tomorrow"
	SnoozeMonday   = "monday"
	SnoozeMonth    = "month"
	SnoozeWeek     = "week"
)

// SnoozeDays turns a snooze choice into days from now: "monday" is the
// next Monday (a week on a Monday), "month" the 1st of next month.
// 0 for an unknown choice.
func SnoozeDays(choice string, now time.Time) int {
	switch choice {
	case SnoozeTomorrow:
		return 1
	case SnoozeWeek:
		return daysPerWeek
	case SnoozeMonday:
		ahead := (int(time.Monday) - int(now.Weekday()) + daysPerWeek) % daysPerWeek
		if ahead == 0 {
			ahead = daysPerWeek
		}
		return ahead
	case SnoozeMonth:
		first := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return int(first.Sub(today).Hours() / 24)
	}
	return 0
}

const daysPerWeek = 7

// Prune deletes hints resolved more than resolvedRetention ago.
func Prune(d *sql.DB) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		return data.PruneResolved(tx, time.Now().UTC().Add(-resolvedRetention))
	})
}
