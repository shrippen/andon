// Package timer starts, stops and edits Kimai timesheets from a board and
// keeps the viewer's pinned pairs.
//
//	tile ──POST──► Run: widget is a Kimai timer? EDIT on the connection?
//	               ──► outbound.Kimai* ──► cache dropped
//	     ──POST──► Pin: pair from the live view ──► user prefs "kimai_favs"
package timer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// WidgetType is the widget key the timer actions belong to.
const WidgetType = "kimai_timer"

// ErrNotTimer means the placement is not a Kimai timer tile.
var ErrNotTimer = errors.New("timer.not_timer")

// ErrBadRange means an entry's end is not after its begin (or a time is
// missing).
var ErrBadRange = errors.New("timer.bad_range")

// ErrBadSplit means the split time is not inside the entry.
var ErrBadSplit = errors.New("timer.bad_split")

// ErrRejected means Kimai refused the write (validation, permission).
var ErrRejected = errors.New("timer.rejected")

// maxFavs caps the pinned pairs per connection.
const maxFavs = 8

// formTime is the add-entry form's local time (datetime-local input);
// kimaiTime what Kimai's API reads.
const (
	formTime  = "2006-01-02T15:04"
	kimaiTime = "2006-01-02T15:04:05"
)

// Action is what a tile button asks for.
type Action string

const (
	ActionStart  Action = "start"
	ActionStop   Action = "stop"
	ActionSwitch Action = "switch"
	ActionCreate Action = "create"
	ActionEdit   Action = "edit"
	ActionDelete Action = "delete"
	ActionSplit  Action = "split"
)

// Request is what a tile button asks for: start (project, activity),
// stop (sheet, with an optional note as description), switch (stop
// sheet, then start the pair), create (an entry from Begin to End, local
// times like "2026-09-26T09:05", with Note, Tags, Billable), edit (sheet
// with the same fields; no End keeps it running), delete (sheet) or
// split (sheet, in two at At).
type Request struct {
	Action            Action
	Project, Activity int64
	Sheet             int64
	Note              string
	StartNote         string // description for a timer being started
	Begin, End        string
	Tags              string // comma-separated: "Schnitt, Ton"
	Billable          enums.Billable
	At                string // split time, like Begin
}

// Run starts, stops, switches, books, edits, deletes or splits a
// timesheet behind a tile. Requires EDIT on the connection.
func Run(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64, req Request, ip string) error {
	conn, secret, err := target(d, who, placementID)
	if err != nil {
		return err
	}
	if err := connections.Writable(d, who, conn.ID); err != nil {
		return err
	}
	to := outbound.Target{URL: conn.URL, Token: secret, VerifyTLS: conn.VerifyTLS}
	stop := func() error {
		if req.Sheet <= 0 {
			return ErrNotTimer
		}
		if req.Note != "" {
			if err := outbound.KimaiDescribe(ctx, to, req.Sheet, req.Note); err != nil {
				return err
			}
		}
		return outbound.KimaiStop(ctx, to, req.Sheet)
	}
	start := func() error {
		if req.Project <= 0 || req.Activity <= 0 {
			return ErrNotTimer
		}
		return outbound.KimaiStart(ctx, to, req.Project, req.Activity, req.StartNote)
	}

	switch req.Action {
	case ActionStart:
		err = start()
	case ActionCreate:
		err = create(ctx, to, req)
	case ActionEdit:
		err = edit(ctx, to, req)
	case ActionDelete:
		err = remove(ctx, to, req)
	case ActionSplit:
		err = split(ctx, d, who, conn, to, req)
	case ActionStop:
		err = stop()
	case ActionSwitch:
		if err = stop(); err == nil {
			err = start()
		}
	default:
		return ErrNotTimer
	}
	if err != nil {
		return rejected(err)
	}
	svcdata.Forget(conn.ID)
	return auditsvc.Log(d, &who.UserID, "kimai."+string(req.Action), strconv.FormatInt(max(req.Project, req.Sheet), 10), ip, nil)
}

// rejected marks Kimai's refusals so a form can say so; our own checks
// pass unchanged.
func rejected(err error) error {
	if errors.Is(err, ErrNotTimer) || errors.Is(err, ErrBadRange) || errors.Is(err, ErrBadSplit) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrRejected, err)
}

// sheetOf turns a form request into a Kimai write, checking the pair and
// the range. End may be empty (running entry).
func sheetOf(req Request) (outbound.KimaiSheet, error) {
	begin, err := time.Parse(formTime, req.Begin)
	if err != nil {
		return outbound.KimaiSheet{}, ErrBadRange
	}
	sheet := outbound.KimaiSheet{Project: req.Project, Activity: req.Activity, Begin: begin.Format(kimaiTime),
		Description: req.Note, Tags: tagsOf(req.Tags)}
	if req.Billable == enums.BillableYes || req.Billable == enums.BillableNo {
		sheet.Billable = req.Billable
	}
	if req.End != "" {
		end, err := time.Parse(formTime, req.End)
		if err != nil || !end.After(begin) {
			return outbound.KimaiSheet{}, ErrBadRange
		}
		sheet.End = end.Format(kimaiTime)
	}
	if req.Project <= 0 || req.Activity <= 0 {
		return outbound.KimaiSheet{}, ErrNotTimer
	}
	return sheet, nil
}

// create books a finished entry.
func create(ctx context.Context, to outbound.Target, req Request) error {
	if req.End == "" {
		return ErrBadRange
	}
	sheet, err := sheetOf(req)
	if err != nil {
		return err
	}
	outbound.KimaiTags(ctx, to, sheet.Tags)
	return outbound.KimaiCreate(ctx, to, sheet)
}

// edit rewrites a running or stopped entry.
func edit(ctx context.Context, to outbound.Target, req Request) error {
	if req.Sheet <= 0 {
		return ErrNotTimer
	}
	sheet, err := sheetOf(req)
	if err != nil {
		return err
	}
	outbound.KimaiTags(ctx, to, sheet.Tags)
	return outbound.KimaiEdit(ctx, to, req.Sheet, sheet)
}

func remove(ctx context.Context, to outbound.Target, req Request) error {
	if req.Sheet <= 0 {
		return ErrNotTimer
	}
	return outbound.KimaiDelete(ctx, to, req.Sheet)
}

// split cuts one of today's stopped entries in two at At; the second
// part copies project, activity, description, tags and billable.
//
//	09:00 ────────── 12:00   split at 10:30
//	09:00 ── 10:30 | 10:30 ── 12:00
func split(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection, to outbound.Target, req Request) error {
	day, err := dayOf(ctx, d, who, conn, svcdata.Force)
	if err != nil {
		return err
	}
	s := day.Sheet(req.Sheet)
	if s == nil || s.End.IsZero() {
		return ErrNotTimer
	}

	// Compare in Kimai's own clock: its times carry the user's offset.
	at, err := time.ParseInLocation(formTime, req.At, s.Begin.Location())
	if err != nil || !at.After(s.Begin) || !at.Before(s.End) {
		return ErrBadSplit
	}
	billable := enums.BillableNo
	if s.Billable {
		billable = enums.BillableYes
	}
	first := outbound.KimaiSheet{Project: s.ProjectID, Activity: s.ActivityID, Begin: s.Begin.Format(kimaiTime),
		End: at.Format(kimaiTime), Description: s.Description, Tags: s.Tags}
	if err := outbound.KimaiEdit(ctx, to, s.ID, first); err != nil {
		return err
	}
	second := first
	second.Begin, second.End, second.Billable = at.Format(kimaiTime), s.End.Format(kimaiTime), billable
	return outbound.KimaiCreate(ctx, to, second)
}

// tagsOf: " Schnitt, ,Ton " → ["Schnitt", "Ton"].
func tagsOf(raw string) []string {
	var out []string
	for _, tag := range strings.Split(raw, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// target resolves the tile's connection and the viewer's secret for it.
func target(d *sql.DB, who *access.Principal, placementID int64) (*model.Connection, string, error) {
	w, err := boards.PlacedWidget(d, who, placementID)
	if err != nil {
		return nil, "", err
	}
	if w.Type != WidgetType || w.ConnectionID == nil {
		return nil, "", ErrNotTimer
	}

	// Seeing a board is not enough to act: the viewer must use the connection.
	if _, err := connections.Get(d, who, *w.ConnectionID); err != nil {
		return nil, "", err
	}
	conn, err := connections.ByID(d, *w.ConnectionID)
	if err != nil {
		return nil, "", err
	}
	secret, err := svcdata.Secret(d, conn, model.UserHolder(who.UserID))
	return conn, secret, err
}

// Catalog lists the projects and activities the tile's Kimai offers for
// a new entry (cached for a few minutes).
func Catalog(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64) (*sources.KimaiCatalog, error) {
	conn, _, err := target(d, who, placementID)
	if err != nil {
		return nil, err
	}
	res, err := load(ctx, d, who, conn, catalogSource, svcdata.Cached)
	if err != nil {
		return nil, err
	}
	catalog, ok := res.(*sources.KimaiCatalog)
	if !ok {
		return nil, ErrNotTimer
	}
	return catalog, nil
}

// Day lists the viewer's timesheets of today behind a tile.
func Day(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64) (*sources.KimaiDay, error) {
	conn, _, err := target(d, who, placementID)
	if err != nil {
		return nil, err
	}
	return dayOf(ctx, d, who, conn, svcdata.Cached)
}

// Draft fills the edit form for a timesheet: the running one or one of
// today's. A running sheet has no End.
func Draft(ctx context.Context, d *sql.DB, who *access.Principal, placementID, sheetID int64) (Request, error) {
	conn, _, err := target(d, who, placementID)
	if err != nil {
		return Request{}, err
	}
	live, err := liveOf(ctx, d, who, conn)
	if err != nil {
		return Request{}, err
	}
	for _, t := range live.Active {
		if t.ID == sheetID {
			return draftOf(t), nil
		}
	}
	day, err := dayOf(ctx, d, who, conn, svcdata.Cached)
	if err != nil {
		return Request{}, err
	}
	if s := day.Sheet(sheetID); s != nil {
		return draftOf(*s), nil
	}
	return Request{}, ErrNotTimer
}

// draftOf keeps Kimai's own clock: its times carry the user's offset.
func draftOf(t sources.KimaiTimer) Request {
	req := Request{Action: ActionEdit, Sheet: t.ID, Project: t.ProjectID, Activity: t.ActivityID, Note: t.Description,
		Begin: t.Begin.Format(formTime), Tags: strings.Join(t.Tags, ", "), Billable: enums.BillableNo}
	if !t.End.IsZero() {
		req.End = t.End.Format(formTime)
	}
	if t.Billable {
		req.Billable = enums.BillableYes
	}
	return req
}

// Pin pins a pair from the tile's running or recent timers, or unpins it
// if pinned. The names are taken from Kimai, not from the request.
func Pin(ctx context.Context, d *sql.DB, who *access.Principal, placementID, projectID, activityID int64) error {
	conn, _, err := target(d, who, placementID)
	if err != nil {
		return err
	}
	live, err := liveOf(ctx, d, who, conn)
	if err != nil {
		return err
	}
	var fav *widgets.KimaiFav
	for _, t := range slices.Concat(live.Active, live.Recent) {
		if t.ProjectID == projectID && t.ActivityID == activityID {
			fav = &widgets.KimaiFav{ProjectID: projectID, ActivityID: activityID, Project: t.Project, Activity: t.Activity,
				Customer: t.Customer, Color: t.Color}
			break
		}
	}

	key := strconv.FormatInt(conn.ID, 10)
	return db.WithTx(d, func(tx *sql.Tx) error {
		u, err := users.Get(tx, who.UserID)
		if err != nil {
			return err
		}
		if u == nil {
			return sql.ErrNoRows
		}
		all, _ := u.Prefs[widgets.KimaiFavsPref].(map[string]any)
		if all == nil {
			all = map[string]any{}
		}
		favs := widgets.KimaiFavsOf(all[key])

		// Pinned → unpin (even if Kimai no longer lists it); else pin.
		i := slices.IndexFunc(favs, func(f widgets.KimaiFav) bool { return f.ProjectID == projectID && f.ActivityID == activityID })
		switch {
		case i >= 0:
			favs = slices.Delete(favs, i, i+1)
		case fav == nil || len(favs) >= maxFavs:
			return ErrNotTimer
		default:
			favs = append(favs, *fav)
		}

		all[key] = favs
		if u.Prefs == nil {
			u.Prefs = map[string]any{}
		}
		u.Prefs[widgets.KimaiFavsPref] = all
		return users.Update(tx, u)
	})
}

// liveOf is the tile connection's live view (cached).
func liveOf(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection) (*sources.KimaiLive, error) {
	res, err := load(ctx, d, who, conn, liveSource, svcdata.Cached)
	if err != nil {
		return nil, err
	}
	live, ok := res.(*sources.KimaiLive)
	if !ok {
		return nil, ErrNotTimer
	}
	return live, nil
}

func dayOf(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection, fresh svcdata.Freshness) (*sources.KimaiDay, error) {
	res, err := load(ctx, d, who, conn, daySource, fresh)
	if err != nil {
		return nil, err
	}
	day, ok := res.(*sources.KimaiDay)
	if !ok {
		return nil, ErrNotTimer
	}
	return day, nil
}

// load runs one Kimai source for the viewer.
func load(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection, source string, fresh svcdata.Freshness) (any, error) {
	res, err := svcdata.Get(ctx, d, source, nil, conn, model.UserHolder(who.UserID), fresh)
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return res.Data, nil
}

// Source keys of Kimai's projects and activities, the live view and the
// day list.
const (
	catalogSource = "kimai.catalog"
	liveSource    = "kimai.live"
	daySource     = "kimai.day"
)
