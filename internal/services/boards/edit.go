package boards

import (
	"andon/internal/services/themes"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/util"
)

// ── Board changes (EDIT) ──

// MaxNameLen bounds a board's name in characters: it stands in the nav
// and in every page title.
const MaxNameLen = 80

// boardName is a typed name trimmed and cut to MaxNameLen.
// ErrNameTaken: another board of the space has this name.
var ErrNameTaken = errors.New("board.name_taken")

func boardName(name string) string {
	runes := []rune(strings.TrimSpace(name))
	return strings.TrimSpace(string(runes[:min(len(runes), MaxNameLen)]))
}

// Create adds a board (with one empty section) to a space. Requires EDIT.
func Create(d *sql.DB, who *access.Principal, spaceID int64, name string) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		space, err := access.SpaceOf(tx, who, spaceID)
		if err != nil {
			return err
		}
		if err := access.Need(access.SpaceRight(who, space), enums.RightEdit); err != nil {
			return err
		}
		existing, err := content.Boards(tx, []int64{spaceID})
		if err != nil {
			return err
		}
		taken, names := map[string]bool{}, map[string]bool{}
		for _, b := range existing {
			taken[b.Slug] = true
			names[strings.ToLower(b.Name)] = true
		}
		label := boardName(name)
		if label == "" {
			label = "Board"
		}
		// A name taken in the space gets a number: "Homelab 2".
		for n, base := 2, label; names[strings.ToLower(label)]; n++ {
			label = base + " " + strconv.Itoa(n)
		}
		board := &model.Board{
			SpaceID: spaceID, Slug: util.Unique(util.Slug(label, startSlug), taken), Name: label,
			Position: len(taken), Version: 1, UpdatedAt: time.Now().UTC(),
		}
		if err := content.AddBoard(tx, board); err != nil {
			return err
		}
		if err := content.AddSection(tx, &model.Section{
			BoardID: board.ID, Size: enums.TileMedium, Sort: enums.SortManual, Area: areas[0],
		}); err != nil {
			return err
		}
		id = board.ID
		return snapshot(tx, who, board)
	})
	return id, err
}

// Rename updates a board's name/theme/team restriction/layout.
func Rename(d *sql.DB, who *access.Principal, boardID int64, version int, name string, themeID *int64, minRole *enums.TeamRole, layout enums.BoardLayout) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if err := bump(board, version); err != nil {
			return err
		}
		if n := boardName(name); n != "" && n != board.Name {
			others, err := content.Boards(tx, []int64{board.SpaceID})
			if err != nil {
				return err
			}
			for _, b := range others {
				if b.ID != board.ID && strings.EqualFold(b.Name, n) {
					return ErrNameTaken
				}
			}
			board.Name = n
		}
		if themeID != nil {
			if err := themes.Usable(tx, who, *themeID); err != nil {
				return err
			}
		}
		board.ThemeID = themeID
		board.MinTeamRole = minRole
		board.Layout = boardLayout(layout)
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
}

// boardLayout accepts only known layouts; anything else is the grid.
func boardLayout(raw enums.BoardLayout) enums.BoardLayout {
	if raw == enums.LayoutMasonry {
		return raw
	}
	return enums.LayoutGrid
}

// Delete removes a board and its shares. Requires MANAGE.
func Delete(d *sql.DB, who *access.Principal, boardID int64) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightManage)
		if err != nil {
			return err
		}
		if err := misc.DropShares(tx, enums.ResourceBoard, board.ID); err != nil {
			return err
		}
		return content.RemoveBoard(tx, board.ID)
	})
}

func bump(board *model.Board, version int) error {
	if board.Version != version {
		return ErrConflict
	}
	board.Version++
	return nil
}

// AddSection appends a new section to a board.
func AddSection(d *sql.DB, who *access.Principal, boardID int64, version int, title string) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if err := bump(board, version); err != nil {
			return err
		}
		section := &model.Section{
			BoardID: board.ID, Title: strings.TrimSpace(title), Position: len(board.Sections),
			Size: enums.TileMedium, Sort: enums.SortManual, Area: areas[0],
		}
		if err := content.AddSection(tx, section); err != nil {
			return err
		}
		id = section.ID
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
	return id, err
}

// SectionChanges lists the mutable fields EditSection may set.
type SectionChanges struct {
	Title     *string
	Cols      **int
	Size      *enums.TileSize
	Sort      *enums.SortOrder
	Collapsed *bool
	Area      *string
	Span      *int
	Rows      *int
	Color     *string
	Icon      *string
	Mobile    *enums.MobileMode
}

// EditSection applies changes to one section.
func EditSection(d *sql.DB, who *access.Principal, sectionID int64, version int, changes SectionChanges) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		section, err := content.Section(tx, sectionID)
		if err != nil {
			return err
		}
		if section == nil {
			return ErrNotFound
		}
		board, err := load(tx, who, section.BoardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if err := bump(board, version); err != nil {
			return err
		}
		if changes.Title != nil {
			section.Title = *changes.Title
		}
		if changes.Cols != nil {
			section.Cols = *changes.Cols
		}
		if changes.Size != nil {
			section.Size = *changes.Size
		}
		if changes.Sort != nil {
			section.Sort = *changes.Sort
		}
		if changes.Collapsed != nil {
			section.Collapsed = *changes.Collapsed
		}
		if changes.Area != nil {
			section.Area = *changes.Area
		}
		if changes.Span != nil {
			section.Span = clampLayout(*changes.Span, MaxSpan)
		}
		if changes.Rows != nil {
			section.Rows = clampLayout(*changes.Rows, MaxRows)
		}
		if changes.Color != nil {
			section.Color = sectionColor(*changes.Color)
		}
		if changes.Icon != nil {
			section.Icon = strings.TrimSpace(*changes.Icon)
		}
		if changes.Mobile != nil {
			section.Mobile = mobileMode(*changes.Mobile)
		}
		if err := content.UpdateSection(tx, section); err != nil {
			return err
		}
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
}

// DeleteSection removes a section and its placements.
func DeleteSection(d *sql.DB, who *access.Principal, sectionID int64, version int) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		section, err := content.Section(tx, sectionID)
		if err != nil || section == nil {
			return err
		}
		board, err := load(tx, who, section.BoardID, enums.RightEdit)
		if err != nil {
			return err
		}
		if err := bump(board, version); err != nil {
			return err
		}
		if err := content.RemoveSection(tx, section.ID); err != nil {
			return err
		}
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
}

// Place puts a library widget on a board (needs USE on the widget).
func Place(d *sql.DB, who *access.Principal, sectionID, widgetID int64, version int) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		section, err := content.Section(tx, sectionID)
		if err != nil {
			return err
		}
		if section == nil {
			return ErrNotFound
		}
		board, err := load(tx, who, section.BoardID, enums.RightEdit)
		if err != nil {
			return err
		}
		id, err = placeIn(tx, who, board, section, widgetID, version)
		return err
	})
	return id, err
}

// placeIn appends a widget (USE needed) to a section of board, which the
// caller loaded with EDIT, as one revision.
func placeIn(tx *sql.Tx, who *access.Principal, board *model.Board, section *model.Section, widgetID int64, version int) (int64, error) {
	widget, err := content.Widget(tx, widgetID)
	if err != nil {
		return 0, err
	}
	if widget == nil {
		return 0, ErrNotFound
	}
	granted, err := widgetRight(tx, who, widget)
	if err != nil {
		return 0, err
	}
	if err := access.Need(granted, enums.RightUse); err != nil {
		return 0, err
	}
	if err := bump(board, version); err != nil {
		return 0, err
	}
	placement := &model.Placement{SectionID: section.ID, WidgetID: widget.ID, Position: len(section.Placements)}
	if err := content.AddPlacement(tx, placement); err != nil {
		return 0, err
	}
	board.UpdatedAt = time.Now().UTC()
	if err := content.UpdateBoard(tx, board); err != nil {
		return 0, err
	}
	return placement.ID, snapshot(tx, who, board)
}

// Unplace removes a widget from a board.
func Unplace(d *sql.DB, who *access.Principal, placementID int64, version int) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		placement, err := content.Placement(tx, placementID)
		if err != nil || placement == nil {
			return err
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
		if err := content.RemovePlacement(tx, placement.ID); err != nil {
			return err
		}
		board.UpdatedAt = time.Now().UTC()
		if err := content.UpdateBoard(tx, board); err != nil {
			return err
		}
		return snapshot(tx, who, board)
	})
}

// Arrange applies a drag-and-drop result {section_id: [placement ids]}.
// Editors in edit mode (want LayoutBoard) reorder the board itself (tiles
// may move between sections); everybody else, and editors in their own
// layout, store the order in their overlay (within a section). It returns
// the board's version afterwards, so the page can carry on.
func Arrange(d *sql.DB, who *access.Principal, boardID int64, version int, layout map[int64][]int64, want LayoutTarget) (LayoutTarget, int, error) {
	var target LayoutTarget
	after := version
	err := db.WithTx(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightView)
		if err != nil {
			return err
		}
		after = board.Version
		known := map[int64]model.Placement{}
		sections := map[int64]bool{}
		for _, sec := range board.Sections {
			sections[sec.ID] = true
			for _, p := range sec.Placements {
				known[p.ID] = p
			}
		}
		for sid, pids := range layout {
			if !sections[sid] {
				return ErrNotFound
			}
			for _, pid := range pids {
				if _, ok := known[pid]; !ok {
					return ErrNotFound
				}
			}
		}

		granted, err := boardRight(tx, who, board)
		if err != nil {
			return err
		}
		if want == LayoutBoard && granted >= enums.RightEdit {
			if err := bump(board, version); err != nil {
				return err
			}
			after = board.Version
			for sectionID, pids := range layout {
				for index, pid := range pids {
					if err := content.UpdatePlacementPosition(tx, pid, sectionID, index); err != nil {
						return err
					}
				}
			}
			board.UpdatedAt = time.Now().UTC()
			if err := content.UpdateBoard(tx, board); err != nil {
				return err
			}
			target = LayoutBoard
			return snapshot(tx, who, board)
		}

		layer, err := overlayOf(tx, who, board.ID)
		if err != nil {
			return err
		}
		data := layer.Data
		orderRaw, _ := data["order"].(map[string]any)
		if orderRaw == nil {
			orderRaw = map[string]any{}
		}
		for sectionID, pids := range layout {
			var kept []any
			for _, pid := range pids {
				if known[pid].SectionID == sectionID {
					kept = append(kept, pid)
				}
			}
			orderRaw[strconv.FormatInt(sectionID, 10)] = kept
		}
		data["order"] = orderRaw
		target = LayoutOverlay
		return content.SetOverlay(tx, who.UserID, board.ID, data)
	})
	return target, after, err
}
