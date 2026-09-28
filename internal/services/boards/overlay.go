package boards

import (
	"database/sql"
	"sort"
	"strconv"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
)

// ── Personal overlay ──

func overlayOf(q db.Queryer, who *access.Principal, boardID int64) (*model.Overlay, error) {
	found, err := content.Overlay(q, who.UserID, boardID)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}
	if err := content.SetOverlay(q, who.UserID, boardID, map[string]any{}); err != nil {
		return nil, err
	}
	return &model.Overlay{UserID: who.UserID, BoardID: boardID, Data: map[string]any{}}, nil
}

// Fold is the collapsed state a section's overlay may hold.
type Fold string

const (
	FoldOpen   Fold = "open"
	FoldClosed Fold = "closed"
)

// Visibility is the hidden state a placement's overlay may hold.
type Visibility string

const (
	VisShown  Visibility = "shown"
	VisHidden Visibility = "hidden"
)

func setLayer(d *sql.DB, who *access.Principal, boardID int64, key string, id int64, value any) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		if _, err := load(tx, who, boardID, enums.RightView); err != nil {
			return err
		}
		overlay, err := content.Overlay(tx, who.UserID, boardID)
		if err != nil {
			return err
		}
		data := map[string]any{}
		if overlay != nil {
			data = overlay.Data
		}

		if key == "hidden" {
			hidden := map[int64]bool{}
			if list, ok := data["hidden"].([]any); ok {
				for _, v := range list {
					hidden[int64FromAny(v)] = true
				}
			}
			if b, _ := value.(bool); b {
				hidden[id] = true
			} else {
				delete(hidden, id)
			}
			ids := make([]int64, 0, len(hidden))
			for k := range hidden {
				ids = append(ids, k)
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			list := make([]any, len(ids))
			for i, v := range ids {
				list[i] = v
			}
			data["hidden"] = list
		} else {
			entry, _ := data[key].(map[string]any)
			if entry == nil {
				entry = map[string]any{}
			}
			entry[strconv.FormatInt(id, 10)] = value
			data[key] = entry
		}
		return content.SetOverlay(tx, who.UserID, boardID, data)
	})
}

// FoldSection sets a section's collapsed state in the caller's overlay.
func FoldSection(d *sql.DB, who *access.Principal, boardID, sectionID int64, state Fold) error {
	return setLayer(d, who, boardID, "collapsed", sectionID, state == FoldClosed)
}

// ShowTile sets a placement's hidden state in the caller's overlay.
func ShowTile(d *sql.DB, who *access.Principal, boardID, placementID int64, state Visibility) error {
	return setLayer(d, who, boardID, "hidden", placementID, state == VisHidden)
}

// ResizeSection sets a section's tile size in the caller's overlay.
func ResizeSection(d *sql.DB, who *access.Principal, boardID, sectionID int64, size enums.TileSize) error {
	return setLayer(d, who, boardID, "size", sectionID, string(size))
}

// Tile heights: a placed tile spans one row of its section's grid, or
// two (MaxTileRows) for a tall one.
const (
	MaxTileRows = 2
	layerRows   = "rows"
)

// tileRows keeps a stored height within 1..MaxTileRows.
func tileRows(rows int) int {
	return min(max(rows, 1), MaxTileRows)
}

// SetTileRows sets how many rows a placed tile spans for everybody.
// Requires EDIT; bumps the board version and records a revision.
func SetTileRows(d *sql.DB, who *access.Principal, placementID int64, rows, version int) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		placement, err := content.Placement(tx, placementID)
		if err != nil || placement == nil {
			return orNotFound(err)
		}
		section, err := content.Section(tx, placement.SectionID)
		if err != nil || section == nil {
			return orNotFound(err)
		}
		board, err := load(tx, who, section.BoardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if err := bump(board, version); err != nil {
			return err
		}
		if err := content.UpdatePlacementRows(tx, placement.ID, tileRows(rows)); err != nil {
			return err
		}
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
}

// SetMyTileRows sets a placed tile's height in the caller's overlay.
func SetMyTileRows(d *sql.DB, who *access.Principal, boardID, placementID int64, rows int) error {
	return setLayer(d, who, boardID, layerRows, placementID, tileRows(rows))
}

// ResetOverlay clears the caller's overlay for a board.
func ResetOverlay(d *sql.DB, who *access.Principal, boardID int64) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		found, err := content.Overlay(tx, who.UserID, boardID)
		if err != nil || found == nil {
			return err
		}
		return content.RemoveOverlay(tx, who.UserID, boardID)
	})
}
