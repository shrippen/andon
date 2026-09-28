package boards

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
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
	Mobile    string  `json:"mobile,omitempty"`
	Widgets   []int64 `json:"widgets"`
	Tall      []int64 `json:"tall,omitempty"` // widgets of Widgets placed MaxTileRows high
}

type snapshotBoard struct {
	Name     string            `json:"name"`
	Sections []snapshotSection `json:"sections"`
}

func snapshot(q db.Queryer, who *access.Principal, board *model.Board) error {
	fresh, err := content.Board(q, board.ID)
	if err != nil || fresh == nil {
		return orNotFound(err)
	}
	snap := snapshotBoard{Name: fresh.Name}
	for _, sec := range fresh.Sections {
		row := snapshotSection{
			Title: sec.Title, Cols: sec.Cols, Size: string(sec.Size), Sort: string(sec.Sort),
			Collapsed: sec.Collapsed, Area: sec.Area, Span: sec.Span, Rows: sec.Rows, Color: sec.Color, Mobile: string(sec.Mobile),
		}
		for _, p := range sec.Placements {
			row.Widgets = append(row.Widgets, p.WidgetID)
			if p.Rows > 1 {
				row.Tall = append(row.Tall, p.WidgetID)
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

// RevisionView is one stored revision.
type RevisionView struct {
	ID      int64
	Version int
	At      time.Time
	UserID  *int64
	Data    map[string]any
}

// History lists a board's revisions, newest first. Requires EDIT.
func History(d *sql.DB, who *access.Principal, boardID int64) ([]RevisionView, error) {
	var out []RevisionView
	err := db.WithTx(d, func(tx *sql.Tx) error {
		if _, err := load(tx, who, boardID, enums.RightEdit); err != nil {
			return err
		}
		revs, err := content.Revisions(tx, enums.RevisionBoard, boardID)
		if err != nil {
			return err
		}
		for _, r := range revs {
			out = append(out, RevisionView{ID: r.ID, Version: r.Version, At: r.CreatedAt, UserID: r.UserID, Data: r.Data})
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

		raw, err := json.Marshal(rev.Data)
		if err != nil {
			return err
		}
		var snap snapshotBoard
		if err := json.Unmarshal(raw, &snap); err != nil {
			return err
		}
		if snap.Name != "" {
			board.Name = snap.Name
		}
		board.Version++
		if err := rebuild(tx, board, snap); err != nil {
			return err
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
			Span: clampLayout(sec.Span, MaxSpan), Rows: clampLayout(sec.Rows, MaxRows), Color: sectionColor(sec.Color),
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
			if w == nil || w.SpaceID != board.SpaceID {
				continue // widget gone, or from a space we can't resolve here (see doc comment)
			}
			if err := content.AddPlacement(tx, &model.Placement{
				SectionID: newSection.ID, WidgetID: widgetID, Position: pos, Rows: rowsIn(sec.Tall, widgetID),
			}); err != nil {
				return err
			}
		}
	}

	return content.UpdateBoard(tx, board)
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
func rowsIn(tall []int64, widgetID int64) int {
	for _, id := range tall {
		if id == widgetID {
			return MaxTileRows
		}
	}
	return 1
}
