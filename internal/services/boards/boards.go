// Package boards is what a user sees, and how editors change layouts.
//
//	Board ─ Section ─ Placement → Widget          (shared structure)
//	                     ▲
//	Overlay(user, board): order, hidden, collapsed, size   (personal layer)
//
// Editors (EDIT on the board) change the board itself. Everybody else
// dragging tiles around changes only their overlay.
package boards

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/icons"
	"andon/internal/services/spaces"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

var areas = []string{"main", "side"}

const startSlug = "start"

// LayoutTarget says where Arrange stored a drag-and-drop result.
type LayoutTarget string

const (
	LayoutBoard   LayoutTarget = "board"
	LayoutOverlay LayoutTarget = "overlay"
)

var (
	ErrNotFound = util.ErrNotFound
	ErrConflict = util.ErrConflict
	ErrDenied   = access.ErrDenied
)

// Tile is one placed, viewable widget.
type Tile struct {
	PlacementID int64
	WidgetID    int64
	Type        string
	Title       string
	Template    string
	Category    widgets.Category
	Inline      bool
	RefreshS    int
	Config      any
	Hidden      bool
	Rows        int    // grid rows the tile spans: the board's, or the viewer's overlay
	Cols        int    // grid columns the tile spans, likewise
	IconURL     string // link tiles: cached icon, "" = monogram
	IconEmoji   string // link tiles: emoji instead of an image
	IconGlyph   bool   // single-color icon, inverted on dark themes
	Items       []TileItem
	Frame       widgets.Frame
	FrameIcon   TileIcon // the frame's title icon
	Host        string   // link tiles: host, when another link has the same title
	Placed      int      // how often the widget is on the board; editors see "2×"
}

// TileIcon is a resolved icon: an emoji or an image.
type TileIcon struct {
	Emoji, URL string
	Glyph      bool
}

// IconOf resolves an icon spec ("si-gitea", "🔥", an uploaded icon); zero
// for none.
func IconOf(spec string) TileIcon {
	if spec == "" {
		return TileIcon{}
	}
	icon := TileIcon{Emoji: icons.Emoji(spec), Glyph: icons.Glyph(spec)}
	if icon.Emoji == "" {
		icon.URL = icons.URL(spec, "")
	}
	return icon
}

// TileItem is a link tile's sub-link with its resolved icon.
type TileItem struct {
	Title, URL, IconURL string
}

// SectionView is one section with its visible tiles.
type SectionView struct {
	ID        int64
	Title     string
	Cols      *int
	Size      enums.TileSize
	Sort      enums.SortOrder
	Collapsed bool
	Area      string
	Span      int
	Rows      int
	Color     string
	Mobile    enums.MobileMode
	Tiles     []Tile
}

// HiddenCount is how many of the section's tiles the viewer hid in their
// own layout.
func (s SectionView) HiddenCount() int {
	n := 0
	for _, t := range s.Tiles {
		if t.Hidden {
			n++
		}
	}
	return n
}

// BoardView is a full board as rendered for one viewer.
type BoardView struct {
	ID          int64
	Slug        string
	Name        string
	Space       access.SpaceRef
	Version     int
	ThemeID     *int64
	MinTeamRole *enums.TeamRole
	Layout      enums.BoardLayout
	CanEdit     bool
	HasOverlay  bool
	Sections    []SectionView
	Page        spaces.PageInfo
	Frequent    []Tile // the viewer's most clicked links
}

// BoardRef is a lightweight board reference for listings.
type BoardRef struct {
	ID      int64
	Slug    string
	Name    string
	Space   access.SpaceRef
	CanEdit bool
	Hidden  bool // left out of the viewer's navigation
}

// ── Rights ──

func boardRight(q db.Queryer, who *access.Principal, board *model.Board) (enums.Right, error) {
	if who.TokenBoards != nil && !containsID(who.TokenBoards, board.ID) {
		return enums.RightNone, nil
	}
	space, err := access.SpaceOf(q, who, board.SpaceID)
	if err != nil {
		return enums.RightNone, err
	}
	return access.Right(who, enums.ResourceBoard, board.ID, space, board.MinTeamRole), nil
}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func widgetRight(q db.Queryer, who *access.Principal, w *model.Widget) (enums.Right, error) {
	space, err := access.SpaceOf(q, who, w.SpaceID)
	if err != nil {
		return enums.RightNone, err
	}
	return access.Right(who, enums.ResourceWidget, w.ID, space, w.MinTeamRole), nil
}

// seenRight is the viewing right on a placed widget: a board shared with
// somebody shows its own space's widgets to them, but not widgets from
// other spaces even if the board happens to reference one.
func seenRight(q db.Queryer, who *access.Principal, w *model.Widget, board *model.Board) (enums.Right, error) {
	granted, err := widgetRight(q, who, w)
	if err != nil {
		return enums.RightNone, err
	}
	if w.SpaceID == board.SpaceID && w.MinTeamRole == nil {
		br, err := boardRight(q, who, board)
		if err != nil {
			return enums.RightNone, err
		}
		if br > enums.RightView {
			br = enums.RightView
		}
		if br > granted {
			granted = br
		}
	}
	return granted, nil
}

func load(q db.Queryer, who *access.Principal, boardID int64, required enums.Right) (*model.Board, error) {
	board, err := content.Board(q, boardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, ErrNotFound
	}
	granted, err := boardRight(q, who, board)
	if err != nil {
		return nil, err
	}
	if err := access.Need(granted, required); err != nil {
		return nil, err
	}
	return board, nil
}

// ── Listing ──

// Visible lists the boards who may at least VIEW, personal spaces first.
func Visible(d *sql.DB, who *access.Principal) ([]BoardRef, error) {
	var out []BoardRef
	err := db.WithRead(d, func(tx *sql.Tx) error {
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		found, err := content.Boards(tx, spaceIDs)
		if err != nil {
			return err
		}
		for _, id := range access.GrantedResourceIDs(who, enums.ResourceBoard) {
			if _, inOwn := who.Spaces[id]; inOwn {
				continue
			}
			b, err := content.Board(tx, id)
			if err != nil {
				return err
			}
			if b != nil {
				if _, already := who.Spaces[b.SpaceID]; !already {
					found = append(found, b)
				}
			}
		}

		order := map[enums.SpaceKind]int{enums.SpacePersonal: 0, enums.SpaceTeam: 1, enums.SpaceInstance: 2}
		for _, board := range found {
			if board.IsTemplate {
				continue
			}
			granted, err := boardRight(tx, who, board)
			if err != nil {
				return err
			}
			if granted < enums.RightView {
				continue
			}
			space, err := access.SpaceOf(tx, who, board.SpaceID)
			if err != nil {
				return err
			}
			ref := BoardRef{ID: board.ID, Slug: board.Slug, Name: board.Name, CanEdit: granted >= enums.RightEdit}
			if space != nil {
				ref.Space = *space
			}
			out = append(out, ref)
		}
		sort.SliceStable(out, func(i, j int) bool { return order[out[i].Space.Kind] < order[out[j].Space.Kind] })
		return nil
	})
	return out, err
}

// StartBoard returns the user's start board id (without a preference the
// first in their own order), creating an empty personal one on first visit.
func StartBoard(d *sql.DB, who *access.Principal, preferred *int64) (int64, error) {
	listed, err := Listed(d, who)
	if err != nil {
		return 0, err
	}
	if preferred != nil {
		for _, b := range listed {
			if b.ID == *preferred {
				return *preferred, nil
			}
		}
	}
	if len(listed) > 0 {
		return listed[0].ID, nil
	}

	personal := access.Personal(who)
	if personal == nil {
		return 0, ErrNotFound
	}
	return Create(d, who, personal.ID, "Start")
}

// ── View ──

// View renders a board for who, applying their personal overlay.
func View(d *sql.DB, who *access.Principal, boardID int64) (*BoardView, error) {
	var out *BoardView
	err := db.WithRead(d, func(tx *sql.Tx) error {
		board, err := load(tx, who, boardID, enums.RightView)
		if err != nil {
			return err
		}
		granted, err := boardRight(tx, who, board)
		if err != nil {
			return err
		}
		overlay, err := content.Overlay(tx, who.UserID, board.ID)
		if err != nil {
			return err
		}
		layer := map[string]any{}
		if overlay != nil {
			layer = overlay.Data
		}
		space, err := access.SpaceOf(tx, who, board.SpaceID)
		if err != nil {
			return err
		}
		view := &BoardView{
			ID: board.ID, Slug: board.Slug, Name: board.Name, Version: board.Version, ThemeID: board.ThemeID,
			Layout: board.Layout, CanEdit: granted >= enums.RightEdit, HasOverlay: len(layer) > 0,
		}
		if space != nil {
			view.Space = *space
		}
		if sp, err := content.Space(tx, board.SpaceID); err == nil && sp != nil {
			view.Page = spaces.PageOf(sp.Settings)
		}
		for _, section := range board.Sections {
			sv, err := viewSection(tx, who, section, board, layer)
			if err != nil {
				return err
			}
			view.Sections = append(view.Sections, sv)
		}
		markTwins(view.Sections)
		if view.Frequent, err = frequent(tx, who, view.Sections); err != nil {
			return err
		}
		out = view
		return nil
	})
	return out, err
}

func viewSection(q db.Queryer, who *access.Principal, section model.Section, board *model.Board, layer map[string]any) (SectionView, error) {
	key := strconv.FormatInt(section.ID, 10)
	size := section.Size
	if sizes, ok := layer["size"].(map[string]any); ok {
		if v, ok := sizes[key].(string); ok {
			size = enums.TileSize(v)
		}
	}
	collapsed := section.Collapsed
	if collapsedMap, ok := layer["collapsed"].(map[string]any); ok {
		if v, ok := collapsedMap[key].(bool); ok {
			collapsed = v
		}
	}
	area := section.Area
	if !containsStr(areas, area) {
		area = areas[0]
	}

	view := SectionView{ID: section.ID, Title: section.Title, Cols: section.Cols, Size: size, Sort: section.Sort,
		Collapsed: collapsed, Area: area, Span: section.Span, Rows: section.Rows, Color: section.Color, Mobile: section.Mobile}

	myRows, _ := layer[layerRows].(map[string]any)
	myCols, _ := layer[layerCols].(map[string]any)
	hidden := map[int64]bool{}
	if hiddenList, ok := layer["hidden"].([]any); ok {
		for _, v := range hiddenList {
			hidden[int64FromAny(v)] = true
		}
	}

	placements := append([]model.Placement(nil), section.Placements...)
	if orderMap, ok := layer["order"].(map[string]any); ok {
		if order, ok := orderMap[key].([]any); ok {
			rank := map[int64]int{}
			for i, v := range order {
				rank[int64FromAny(v)] = i
			}
			sort.SliceStable(placements, func(i, j int) bool {
				ri, iok := rank[placements[i].ID]
				rj, jok := rank[placements[j].ID]
				if !iok {
					ri = len(rank) + placements[i].Position
				}
				if !jok {
					rj = len(rank) + placements[j].Position
				}
				return ri < rj
			})
		}
	}

	for _, placement := range placements {
		w := placement.Widget
		if w == nil {
			continue
		}
		kind, ok := widgets.Get(w.Type)
		if !ok {
			continue
		}
		granted, err := seenRight(q, who, w, board)
		if err != nil {
			return SectionView{}, err
		}
		if granted < enums.RightView {
			continue
		}
		cfg, _ := widgets.Decode(w.Type, w.Config)
		tile := Tile{
			PlacementID: placement.ID, WidgetID: w.ID, Type: w.Type, Title: w.Title, Template: kind.Template,
			Category: kind.Category, Inline: kind.Inline, RefreshS: kind.RefreshS, Config: cfg, Hidden: hidden[placement.ID],
			Rows: tileRows(placement.Rows), Cols: tileCols(placement.Cols),
		}
		tile.Frame = widgets.FrameOf(w.Config)
		if own, ok := cfg.(widgets.Refresher); ok && own.RefreshSeconds() > 0 {
			tile.RefreshS = own.RefreshSeconds()
		}
		if tile.Frame.RefreshS > 0 {
			tile.RefreshS = tile.Frame.RefreshS
		}
		tile.FrameIcon = IconOf(tile.Frame.Icon)
		if v, ok := myRows[strconv.FormatInt(placement.ID, 10)]; ok {
			tile.Rows = tileRows(int(int64FromAny(v)))
		}
		if v, ok := myCols[strconv.FormatInt(placement.ID, 10)]; ok {
			tile.Cols = tileCols(int(int64FromAny(v)))
		}
		if link, ok := cfg.(widgets.LinkConfig); ok {
			tile.IconEmoji = icons.Emoji(link.Icon)
			tile.IconGlyph = icons.Glyph(link.Icon)
			if tile.IconEmoji == "" {
				tile.IconURL = icons.URL(icons.LinkSpec(link.Icon, w.Title), link.URL)
			}
			for _, item := range link.Items {
				tile.Items = append(tile.Items, TileItem{Title: item.Title, URL: item.URL, IconURL: icons.URL(icons.LinkSpec(item.Icon, item.Title), item.URL)})
			}
		}
		view.Tiles = append(view.Tiles, tile)
	}

	if view.Sort == enums.SortAlphabetical {
		sort.SliceStable(view.Tiles, func(i, j int) bool {
			return strings.ToLower(view.Tiles[i].Title) < strings.ToLower(view.Tiles[j].Title)
		})
	}
	return view, nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

// PlacedWidget returns the widget behind a placement, checking view rights.
func PlacedWidget(d *sql.DB, who *access.Principal, placementID int64) (*model.Widget, error) {
	var w *model.Widget
	err := db.WithRead(d, func(tx *sql.Tx) error {
		placement, err := content.Placement(tx, placementID)
		if err != nil {
			return err
		}
		if placement == nil {
			return ErrNotFound
		}
		section, err := content.Section(tx, placement.SectionID)
		if err != nil || section == nil {
			return orNotFound(err)
		}
		board, err := load(tx, who, section.BoardID, enums.RightView)
		if err != nil {
			return err
		}
		granted, err := seenRight(tx, who, placement.Widget, board)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightView); err != nil {
			return err
		}
		w = placement.Widget
		return nil
	})
	return w, err
}

// Fragment loads one placed widget's live data for lazy tile rendering
// (the board page's own render only shows title/type; the tile then
// hx-gets this to fill in).
func Fragment(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64, fresh svcdata.Freshness) (*widgetlib.Fragment, error) {
	w, err := PlacedWidget(d, who, placementID)
	if err != nil {
		return nil, err
	}
	return widgetlib.Load(ctx, d, who, w, fresh)
}

func orNotFound(err error) error {
	if err != nil {
		return err
	}
	return ErrNotFound
}
