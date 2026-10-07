package boards

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// ── Revisions ──
//
// Snapshots store this board's own sections/placements as JSON, not the
// portable YAML of porting.ExportBoard: a revision restores within this
// dashboard, it doesn't move widgets across spaces.

type snapshotSection struct {
	Title     string  `json:"title"`
	Cols      *int    `json:"cols,omitempty"`
	Size      string  `json:"size"`
	Sort      string  `json:"sort"`
	Collapsed bool    `json:"collapsed"`
	Area      string  `json:"area"`
	Span      int     `json:"span,omitempty"`
	Rows      int     `json:"rows,omitempty"`
	Color     string  `json:"color,omitempty"`
	Icon      string  `json:"icon,omitempty"`
	Mobile    string  `json:"mobile,omitempty"`
	Widgets   []int64 `json:"widgets"`
	Tall      []int64 `json:"tall,omitempty"` // widgets of Widgets placed MaxTileRows high
	Wide      []int64 `json:"wide,omitempty"` // widgets of Widgets placed MaxTileCols wide
}

type snapshotBoard struct {
	Name     string            `json:"name"`
	Sections []snapshotSection `json:"sections"`
	// UndoTo is the revision an undo restored; the next undo goes on
	// from there, one step further back.
	UndoTo int64 `json:"undo_to,omitempty"`
	// Gone keeps a copy of tiles deleted right after this revision, by
	// widget id, so restoring it brings them back.
	Gone map[string]goneWidget `json:"gone,omitempty"`
}

// goneWidget is a deleted tile as a revision keeps it.
type goneWidget struct {
	Key          string          `json:"key"`
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	Config       map[string]any  `json:"config"`
	ConnectionID *int64          `json:"connection_id,omitempty"`
	MinTeamRole  *enums.TeamRole `json:"min_team_role,omitempty"`
}

func snapshot(q db.Queryer, who *access.Principal, board *model.Board) error {
	return snapshotUndo(q, who, board, 0)
}

// snapshotUndo stores the board's state; undoTo names the revision an
// undo restored (0 for any other change).
func snapshotUndo(q db.Queryer, who *access.Principal, board *model.Board, undoTo int64) error {
	return snapshotFull(q, who, board, undoTo, nil)
}

// snapshotFull stores the board's state with copies of tiles about to be
// deleted.
func snapshotFull(q db.Queryer, who *access.Principal, board *model.Board, undoTo int64, gone map[string]goneWidget) error {
	fresh, err := content.Board(q, board.ID)
	if err != nil || fresh == nil {
		return orNotFound(err)
	}
	snap := snapshotBoard{Name: fresh.Name, UndoTo: undoTo, Gone: gone}
	for _, sec := range fresh.Sections {
		row := snapshotSection{
			Title: sec.Title, Cols: sec.Cols, Size: string(sec.Size), Sort: string(sec.Sort),
			Collapsed: sec.Collapsed, Area: sec.Area, Span: sec.Span, Rows: sec.Rows, Color: sec.Color, Icon: sec.Icon, Mobile: string(sec.Mobile),
		}
		for _, p := range sec.Placements {
			row.Widgets = append(row.Widgets, p.WidgetID)
			if p.Rows > 1 {
				row.Tall = append(row.Tall, p.WidgetID)
			}
			if p.Cols > 1 {
				row.Wide = append(row.Wide, p.WidgetID)
			}
		}
		snap.Sections = append(snap.Sections, row)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}

	rev := &model.Revision{
		Kind: enums.RevisionBoard, EntityID: fresh.ID, SpaceID: fresh.SpaceID, UserID: &who.UserID,
		Version: fresh.Version, Data: data, CreatedAt: time.Now().UTC(),
	}
	if err := content.AddRevision(q, rev); err != nil {
		return err
	}
	return content.PruneRevisions(q, enums.RevisionBoard, fresh.ID)
}

// RevisionView is one stored revision, its sections summed up:
// "Links (4), Tools (2)".
type RevisionView struct {
	ID       int64
	UndoTo   int64 // the revision an undo restored, 0 otherwise
	Version  int
	At       time.Time
	UserID   *int64
	Sections []RevisionSection
}

// RevisionSection is one section of a revision and its tile count.
type RevisionSection struct {
	Title   string
	Widgets int
}

// snapshotOf reads a stored revision back into its snapshot.
func snapshotOf(data map[string]any) (snapshotBoard, error) {
	var snap snapshotBoard
	raw, err := json.Marshal(data)
	if err != nil {
		return snap, err
	}
	err = json.Unmarshal(raw, &snap)
	return snap, err
}

// History lists a board's revisions, newest first. Requires EDIT.
func History(d *sql.DB, who *access.Principal, boardID int64) ([]RevisionView, error) {
	var out []RevisionView
	err := db.WithRead(d, func(tx *sql.Tx) error {
		if _, err := load(tx, who, boardID, enums.RightEdit); err != nil {
			return err
		}
		revs, err := content.Revisions(tx, enums.RevisionBoard, boardID)
		if err != nil {
			return err
		}
		for _, r := range revs {
			view := RevisionView{ID: r.ID, Version: r.Version, At: r.CreatedAt, UserID: r.UserID}
			snap, err := snapshotOf(r.Data)
			if err != nil {
				return err
			}
			view.UndoTo = snap.UndoTo
			for _, sec := range snap.Sections {
				view.Sections = append(view.Sections, RevisionSection{Title: sec.Title, Widgets: len(sec.Widgets)})
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// ErrBadRevision means the given revision doesn't belong to this board.
var ErrBadRevision = errors.New("boards: revision does not match board")

// Restore rebuilds sections and placements from a stored revision. Widget
// references outside this board's own space are skipped (see the
// Revisions doc comment above).
func Restore(d *sql.DB, who *access.Principal, boardID, revisionID int64) error {
	return restore(d, who, boardID, revisionID, false)
}

// restore rebuilds the board from a revision; an undo marks the new
// revision with the one it restored.
func restore(d *sql.DB, who *access.Principal, boardID, revisionID int64, undo bool) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightEdit)
		if err != nil {
			return err
		}
		rev, err := content.RevisionByID(tx, revisionID)
		if err != nil {
			return err
		}
		if rev == nil || rev.EntityID != board.ID || rev.Kind != enums.RevisionBoard {
			return ErrBadRevision
		}

		snap, err := snapshotOf(rev.Data)
		if err != nil {
			return err
		}
		if snap.Name != "" {
			board.Name = snap.Name
		}
		board.Version++
		if err := rebuild(tx, board, snap); err != nil {
			return err
		}
		if undo {
			return snapshotUndo(tx, who, board, revisionID)
		}
		return snapshot(tx, who, board)
	})
}

// rebuild replaces a board's sections and placements with those of snap
// and stores the board. Widgets outside the board's space are skipped.
func rebuild(tx *sql.Tx, board *model.Board, snap snapshotBoard) error {
	for _, sec := range board.Sections {
		if err := content.RemoveSection(tx, sec.ID); err != nil {
			return err
		}
	}
	board.UpdatedAt = time.Now().UTC()

	for index, sec := range snap.Sections {
		size := enums.TileSize(sec.Size)
		if size == "" {
			size = enums.TileMedium
		}
		sortOrder := enums.SortOrder(sec.Sort)
		if sortOrder == "" {
			sortOrder = enums.SortManual
		}
		area := sec.Area
		if area == "" {
			area = areas[0]
		}
		newSection := &model.Section{
			BoardID: board.ID, Title: sec.Title, Position: index, Cols: sec.Cols,
			Size: size, Sort: sortOrder, Collapsed: sec.Collapsed, Area: area,
			Span: clampLayout(sec.Span, MaxSpan), Rows: clampLayout(sec.Rows, MaxRows), Color: sectionColor(sec.Color), Icon: strings.TrimSpace(sec.Icon),
			Mobile: mobileMode(enums.MobileMode(sec.Mobile)),
		}
		if err := content.AddSection(tx, newSection); err != nil {
			return err
		}
		for pos, widgetID := range sec.Widgets {
			w, err := content.Widget(tx, widgetID)
			if err != nil {
				return err
			}
			if w == nil {
				// Deleted after this revision: back from its copy.
				if w, err = revive(tx, board.SpaceID, snap.Gone[strconv.FormatInt(widgetID, 10)]); err != nil {
					return err
				}
			}
			if w == nil || w.SpaceID != board.SpaceID {
				continue // widget gone, or from a space we can't resolve here (see doc comment)
			}
			if err := content.AddPlacement(tx, &model.Placement{
				SectionID: newSection.ID, WidgetID: w.ID, Position: pos, Rows: spanIn(sec.Tall, widgetID, MaxTileRows),
				Cols: spanIn(sec.Wide, widgetID, MaxTileCols),
			}); err != nil {
				return err
			}
		}
	}

	return content.UpdateBoard(tx, board)
}

// revive brings a deleted tile back from a revision's copy: the space's
// tile of the same key and type if one came back already (undo on its
// other board), else a new one. Nil without a copy.
func revive(tx *sql.Tx, spaceID int64, g goneWidget) (*model.Widget, error) {
	if g.Type == "" {
		return nil, nil
	}
	same, err := content.WidgetByKey(tx, spaceID, g.Key)
	if err != nil {
		return nil, err
	}
	if same != nil && same.Type == g.Type {
		return same, nil
	}
	key := g.Key
	if same != nil {
		key += "-2"
	}
	w := &model.Widget{SpaceID: spaceID, Key: key, Type: g.Type, Title: g.Title, Config: g.Config,
		ConnectionID: g.ConnectionID, MinTeamRole: g.MinTeamRole, Version: 1, UpdatedAt: time.Now().UTC()}
	if w.ConnectionID != nil {
		if c, err := content.Connection(tx, *w.ConnectionID); err != nil || c == nil {
			w.ConnectionID = nil
		}
	}
	return w, content.AddWidget(tx, w)
}

// DeleteWidget deletes a tile everywhere and returns on how many boards
// it was. Each board keeps a revision with a copy of it first, so its
// undo brings the tile back. Requires MANAGE on the tile.
func DeleteWidget(d *sql.DB, who *access.Principal, widgetID int64) (int, error) {
	var count int
	err := db.WithTx(d, func(tx *sql.Tx) error {
		w, err := content.Widget(tx, widgetID)
		if err != nil || w == nil {
			return orNotFound(err)
		}
		ids, err := content.WidgetBoards(tx, widgetID)
		if err != nil {
			return err
		}
		count = len(ids)
		gone := map[string]goneWidget{strconv.FormatInt(w.ID, 10): {Key: w.Key, Type: w.Type, Title: w.Title,
			Config: w.Config, ConnectionID: w.ConnectionID, MinTeamRole: w.MinTeamRole}}
		var editable []*model.Board
		for _, id := range ids {
			board, err := load(tx, who, id, enums.RightEdit)
			if err != nil {
				continue // a board one may not edit has no undo for one
			}
			if err := snapshotFull(tx, who, board, 0, gone); err != nil {
				return err
			}
			editable = append(editable, board)
		}
		if err := widgetlib.DeleteIn(tx, who, widgetID); err != nil {
			return err
		}
		for _, board := range editable {
			if err := snapshot(tx, who, board); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

// Section layout: the main column is twelve twelfths wide. Span 1–3 are
// quarters, 4 the full width, 5–6 thirds; SpanFlow sections sit in
// newspaper columns with their flowing neighbours (link groups).
const (
	SpanFlow = 7
	MaxSpan  = SpanFlow
	MaxRows  = 4
)

// clampLayout keeps span/rows in 0..max; 0 means the default.
func clampLayout(v, max int) int {
	if v < 0 || v > max {
		return 0
	}
	return v
}

// mobileMode accepts only known modes; anything else shows normally.
func mobileMode(raw enums.MobileMode) enums.MobileMode {
	if raw == enums.MobileFirst || raw == enums.MobileHide {
		return raw
	}
	return enums.MobileNormal
}

// sectionColor accepts only theme color names.
func sectionColor(raw string) string {
	for _, c := range widgets.TileColors {
		if string(c) == raw {
			return raw
		}
	}
	return ""
}

// rowsIn is MaxTileRows for a widget listed as tall, else 1.
// spanIn is span for widgets listed in ids (tall or wide), else 1.
func spanIn(ids []int64, widgetID int64, span int) int {
	if slices.Contains(ids, widgetID) {
		return span
	}
	return 1
}
