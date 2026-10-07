package boards

// Putting tiles on a board from outside its edit mode: from the library
// ("Auf Board legen …"), from an empty board's tips and after a green
// connection test. The tile goes to the end of the board's first section.
//
//	library row ── PutOn(board, widget) ──┐
//	template ───── AddNew(board, type) ───┼─► first section (added if none) ─► one revision
//	empty board ── EmptyTips ─► AddNew ───┘

import (
	"database/sql"
	"sort"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// maxTips caps the starters an empty board offers.
const maxTips = 6

// firstSection is the section a tile from outside goes to: the board's
// first, or a new one when the board has none.
func firstSection(tx *sql.Tx, board *model.Board) (*model.Section, error) {
	if len(board.Sections) > 0 {
		return &board.Sections[0], nil
	}
	section := &model.Section{BoardID: board.ID, Size: enums.TileMedium, Sort: enums.SortManual, Area: areas[0]}
	if err := content.AddSection(tx, section); err != nil {
		return nil, err
	}
	return section, nil
}

// putIn places widgetID on board (loaded with EDIT) as one revision.
func putIn(tx *sql.Tx, who *access.Principal, board *model.Board, widgetID int64) error {
	section, err := firstSection(tx, board)
	if err != nil {
		return err
	}
	_, err = placeIn(tx, who, board, section, widgetID, board.Version)
	return err
}

// PutOn places a library widget (USE) on a board (EDIT).
func PutOn(d *sql.DB, who *access.Principal, boardID, widgetID int64) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightEdit)
		if err != nil {
			return err
		}
		return putIn(tx, who, board, widgetID)
	})
}

// AddNew sets up a tile of typeKey (on connID, if it reads one) in the
// board's space and places it on the board. Requires EDIT on the board
// and its space.
func AddNew(d *sql.DB, who *access.Principal, boardID int64, typeKey string, connID *int64) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if id, err = widgetlib.CreateTx(tx, who, board.SpaceID, typeKey, "", nil, connID, nil); err != nil {
			return err
		}
		return putIn(tx, who, board, id)
	})
	return id, err
}

// EmptyTips suggests starters for an empty board: one per connection of
// its space without a tile yet, by name. None if the board has tiles or
// who may not edit it.
func EmptyTips(d *sql.DB, who *access.Principal, boardID int64) ([]SuggestedTile, error) {
	var out []SuggestedTile
	err := db.WithRead(d, func(tx *sql.Tx) error {
		board, err := content.Board(tx, boardID)
		if err != nil || board == nil {
			return err
		}
		granted, err := boardRight(tx, who, board)
		if err != nil || granted < enums.RightEdit {
			return err
		}
		for _, s := range board.Sections {
			if len(s.Placements) > 0 {
				return nil
			}
		}
		out, err = tipsIn(tx, board.SpaceID)
		return err
	})
	return out, err
}

// tipsIn lists a starter for each connection of a space without a tile.
func tipsIn(tx *sql.Tx, spaceID int64) ([]SuggestedTile, error) {
	ws, err := content.Widgets(tx, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	conns, err := content.Connections(tx, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	used := map[int64]bool{}
	for _, w := range ws {
		if w.ConnectionID != nil {
			used[*w.ConnectionID] = true
		}
	}
	sort.SliceStable(conns, func(a, b int) bool { return conns[a].Name < conns[b].Name })

	var out []SuggestedTile
	for _, c := range conns {
		key, ok := widgets.Starter(enums.ServiceType(c.Service))
		if used[c.ID] || !ok {
			continue
		}
		id := c.ID
		out = append(out, SuggestedTile{Type: key, ConnID: &id, ConnName: c.Name})
		if len(out) == maxTips {
			break
		}
	}
	return out, nil
}

// Fitting lists up to limit templates for a connection (USE) that are not
// set up yet: its own types on it, partner types (e.g. "backups")
// without a connection. Offered after a green connection test.
func Fitting(d *sql.DB, who *access.Principal, connID int64, limit int) ([]SuggestedTile, error) {
	var out []SuggestedTile
	err := db.WithRead(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		space, err := access.SpaceOf(tx, who, conn.SpaceID)
		if err != nil {
			return err
		}
		if err := access.Need(access.Right(who, enums.ResourceConnection, conn.ID, space, nil), enums.RightUse); err != nil {
			return err
		}

		// What is set up already: types on this connection, partner types anywhere.
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		ws, err := content.Widgets(tx, spaceIDs)
		if err != nil {
			return err
		}
		onConn, anywhere := map[string]bool{}, map[string]bool{}
		for _, w := range ws {
			anywhere[w.Type] = true
			if w.ConnectionID != nil && *w.ConnectionID == conn.ID {
				onConn[w.Type] = true
			}
		}

		for _, key := range widgets.ForService(enums.ServiceType(conn.Service)) {
			if len(out) == limit {
				break
			}
			kind, _ := widgets.Get(key)
			if kind.Service == "" {
				if !anywhere[key] {
					out = append(out, SuggestedTile{Type: key})
				}
				continue
			}
			if !onConn[key] {
				id := conn.ID
				out = append(out, SuggestedTile{Type: key, ConnID: &id, ConnName: conn.Name})
			}
		}
		return nil
	})
	return out, err
}
