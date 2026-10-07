package web

import (
	"andon/internal/services/verbund"
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"andon/internal/model"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/themes"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// RegisterEditorRoutes wires board settings, sections, the widget library
// and placement.
func (d Deps) RegisterEditorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /boards/{id}/settings", d.authed(d.handleBoardSettingsForm))
	mux.HandleFunc("POST /boards/{id}/settings", d.authed(d.handleBoardRename))
	mux.HandleFunc("POST /boards/{id}/delete", d.authed(d.handleBoardDelete))
	mux.HandleFunc("POST /boards/{id}/sections", d.authed(d.handleSectionAdd))
	mux.HandleFunc("POST /sections/{id}/edit", d.authed(d.handleSectionEdit))
	mux.HandleFunc("POST /sections/{id}/delete", d.authed(d.handleSectionDelete))
	mux.HandleFunc("POST /boards/{boardID}/sections/{sectionID}/place", d.authed(d.handlePlace))
	mux.HandleFunc("POST /placements/{id}/unplace", d.authed(d.handleUnplace))
	mux.HandleFunc("POST /placements/{id}/rows", d.authed(d.handleTileRows))
	mux.HandleFunc("POST /placements/{id}/cols", d.authed(d.handleTileCols))

	mux.HandleFunc("GET /widgets", d.authed(d.handleWidgetLibrary))
	mux.HandleFunc("GET /widgets/new", d.authed(d.handleWidgetNewForm))
	mux.HandleFunc("POST /widgets", d.authed(d.handleWidgetCreate))
	mux.HandleFunc("GET /widgets/{id}/edit", d.authed(d.handleWidgetEditForm))
	mux.HandleFunc("POST /widgets/{id}/edit", d.authed(d.handleWidgetUpdate))
	mux.HandleFunc("POST /widgets/{id}/delete", d.authed(d.handleWidgetDelete))
	mux.HandleFunc("POST /widget-preview", d.authed(d.handleWidgetPreview))
	mux.HandleFunc("GET /widget-sample/{type}", d.authed(d.handleSample))
	mux.HandleFunc("GET /widgets/{id}/preview", d.authed(d.handleWidgetShow))
	mux.HandleFunc("GET /widget-tiles/{type}", d.authed(d.handleGalleryTiles))
	mux.HandleFunc("POST /widgets/unused/delete", d.authed(d.handleUnusedDelete))
}

func (d Deps) handleBoardSettingsForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view, err := boards.View(d.DB, ctx.Who, id, boards.LayoutOverlay)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	themeList, err := themes.Listing(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "board_settings", http.StatusOK, map[string]any{
		"Board": view, "Themes": themeList, "WallPageMin": boards.WallPageMin, "WallPageMax": boards.WallPageMax,
		"WallTurns": enums.WallTurns, "WallEases": enums.WallEases, "WallSets": previewSets(view),
		"TeamRoles": []enums.TeamRole{enums.TeamViewer, enums.TeamEditor, enums.TeamOwner},
	})
}

func (d Deps) handleBoardRename(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	var themeID *int64
	if n, err := strconv.ParseInt(r.FormValue("theme_id"), 10, 64); err == nil {
		themeID = &n
	}
	if err := boards.Rename(d.DB, ctx.Who, id, version, r.FormValue("name"), themeID, minRole(r), enums.BoardLayout(r.FormValue("layout")), wallForm(r)); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/boards/"+r.PathValue("id"), http.StatusSeeOther)
}

// previewTiles is how many of the board's tiles fill one set of the wall
// display preview (4 × 2 placeholders).
const previewTiles = 8

// previewSets are the two sets of the wall display preview: the board's
// first tiles, then the next ones (or the first ones again).
//
//	board tiles  t1 … t8 | t9 … t16 | …   → set 1: t1–t8, set 2: t9–t16
func previewSets(view *boards.BoardView) [2][]boards.Tile {
	var all []boards.Tile
	for _, s := range view.Sections {
		for _, t := range s.Tiles {
			if !t.Hidden {
				all = append(all, t)
			}
		}
	}
	if len(all) == 0 {
		all = make([]boards.Tile, previewTiles) // an empty board: blank tiles still show the motion
	}
	first := all[:min(len(all), previewTiles)]
	second := all[len(first):min(len(all), 2*previewTiles)]
	if len(second) == 0 {
		second = first
	}
	return [2][]boards.Tile{first, second}
}

// wallForm reads the wall display group of the board settings.
func wallForm(r *http.Request) boards.Wall {
	return boards.Wall{Page: formInt(r, "wall_page"), Turn: enums.WallTurn(r.FormValue("wall_turn")), Ease: enums.WallEase(r.FormValue("wall_ease"))}
}

func (d Deps) handleBoardDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := boards.Delete(d.DB, ctx.Who, id); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d Deps) handleSectionAdd(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	boardID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	if _, err := boards.AddSection(d.DB, ctx.Who, boardID, version, r.FormValue("title")); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/boards/"+r.PathValue("id")+"?edit", http.StatusSeeOther)
}

func (d Deps) handleSectionEdit(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	title := r.FormValue("title")
	size := enums.TileSize(r.FormValue("size"))
	sortOrder := enums.SortOrder(r.FormValue("sort"))
	area := r.FormValue("area")
	collapsed := r.FormValue("collapsed") != ""
	var cols *int
	if n, err := strconv.Atoi(r.FormValue("cols")); err == nil && n > 0 {
		cols = &n
	}
	span := formInt(r, "span")
	rows := formInt(r, "rows")
	color := r.FormValue("color")
	icon := r.FormValue("icon")
	mobile := enums.MobileMode(r.FormValue("mobile"))
	changes := boards.SectionChanges{Title: &title, Size: &size, Sort: &sortOrder, Area: &area,
		Collapsed: &collapsed, Cols: &cols, Span: &span, Rows: &rows, Color: &color, Icon: &icon, Mobile: &mobile}

	boardID := r.FormValue("board_id")
	if err := boards.EditSection(d.DB, ctx.Who, id, version, changes); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}

	// A section that changes area or flows into columns moves on the page:
	// the section alone would land in the wrong place.
	if sectionTarget(r) > 0 && (area != r.FormValue("was_area") || flows(span) != flows(atoi(r.FormValue("was_span")))) {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if d.boardPart(w, r, ctx, formBoard(r), partEdit, hintNone) {
		return
	}
	http.Redirect(w, r, "/boards/"+boardID+"?edit", http.StatusSeeOther)
}

// flows reports a span that puts a section into the flowing columns.
func flows(span int) bool { return span == boards.SpanFlow }

func atoi(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}

func (d Deps) handleSectionDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	boardID := r.FormValue("board_id")
	if err := boards.DeleteSection(d.DB, ctx.Who, id, version); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/boards/"+boardID+"?edit&undo", http.StatusSeeOther)
}

func (d Deps) handlePlace(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	boardID, err := pathID(r, "boardID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sectionID, err := pathID(r, "sectionID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	widgetID := formID(r, "widget_id")
	version := formInt(r, "version")
	if _, err := boards.Place(d.DB, ctx.Who, sectionID, widgetID, version); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/boards/"+strconv.FormatInt(boardID, 10)+"?edit", http.StatusSeeOther)
}

func (d Deps) handleUnplace(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	boardID := r.FormValue("board_id")
	if err := boards.Unplace(d.DB, ctx.Who, id, version); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if d.boardPart(w, r, ctx, formBoard(r), partEdit, hintUndo) {
		return
	}
	http.Redirect(w, r, "/boards/"+boardID+"?edit&undo", http.StatusSeeOther)
}

// handleTileRows sets how many rows a placed tile spans for everybody.
func (d Deps) handleTileRows(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	rows := formInt(r, "rows")
	if err := boards.SetTileRows(d.DB, ctx.Who, id, rows, version); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if d.boardPart(w, r, ctx, formBoard(r), partEdit, hintNone) {
		return
	}
	http.Redirect(w, r, "/boards/"+r.FormValue("board_id")+"?edit", http.StatusSeeOther)
}

// handleTileCols sets how many columns a placed tile spans for everybody.
func (d Deps) handleTileCols(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	version := formInt(r, "version")
	cols := formInt(r, "cols")
	if err := boards.SetTileCols(d.DB, ctx.Who, id, cols, version); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if d.boardPart(w, r, ctx, formBoard(r), partEdit, hintNone) {
		return
	}
	http.Redirect(w, r, "/boards/"+r.FormValue("board_id")+"?edit", http.StatusSeeOther)
}

// ── Widget library ──

func (d Deps) handleWidgetLibrary(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	lib, err := widgetlib.Library(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	seen, err := boards.Visible(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	groups := libraryGroups(ctx, lib, seen)
	_ = d.Page(w, ctx, "widgets", http.StatusOK, map[string]any{"Groups": groups, "Count": len(lib),
		"Tally": tallyLibrary(groups), "Spaces": access.EditableSpaces(ctx.Who)})
}

// libraryGroup is one topic of the library ("" = links), A–Z by name.
type libraryGroup struct {
	Topic widgets.Topic
	Tiles []libraryTile
}

// libraryTile is a library row: where the tile is placed (only boards the
// viewer sees) and whether it lacks its own name ("Kennzahl" eleven times).
type libraryTile struct {
	galleryTile
	Service enums.ServiceType
	Boards  []boards.BoardRef
	Unnamed bool
}

// libraryTally counts the rows per filter: space kind, unnamed, unused.
type libraryTally struct {
	Kinds           map[string]int // by space kind, for the template's index
	Unnamed, Unused int
}

// libraryGroups sorts the library like the gallery: by topic, links last.
func libraryGroups(ctx Ctx, lib []widgetlib.Ref, seen []boards.BoardRef) []libraryGroup {
	sorter := collate.New(language.Make(string(ctx.Locale)), collate.IgnoreCase)
	boardByID := make(map[int64]boards.BoardRef, len(seen))
	for _, b := range seen {
		boardByID[b.ID] = b
	}

	byTopic := map[widgets.Topic][]libraryTile{}
	for _, ref := range lib {
		tile := libraryTile{galleryTile: galleryTile{Ref: ref, Name: cmp.Or(ref.Title, i18n.T("wtype."+ref.Type+".name", ctx.Locale, nil))},
			Unnamed: ref.Title == "" && ref.Type != linkType}
		if kind, ok := widgets.Get(ref.Type); ok {
			tile.Service = kind.Service
		}
		for _, id := range ref.Boards {
			if b, ok := boardByID[id]; ok {
				tile.Boards = append(tile.Boards, b)
			}
		}
		topic := widgets.TopicOf(ref.Type)
		if ref.Type == linkType {
			topic = ""
		}
		byTopic[topic] = append(byTopic[topic], tile)
	}
	var out []libraryGroup
	for _, topic := range append(slices.Clone(widgets.Topics), "") {
		tiles := byTopic[topic]
		if len(tiles) == 0 {
			continue
		}
		slices.SortFunc(tiles, func(a, b libraryTile) int { return sorter.CompareString(a.Name, b.Name) })
		out = append(out, libraryGroup{Topic: topic, Tiles: tiles})
	}
	return out
}

func tallyLibrary(groups []libraryGroup) libraryTally {
	out := libraryTally{Kinds: map[string]int{}}
	for _, g := range groups {
		for _, tile := range g.Tiles {
			out.Kinds[string(tile.Space.Kind)]++
			if tile.Unnamed {
				out.Unnamed++
			}
			if tile.Uses == 0 {
				out.Unused++
			}
		}
	}
	return out
}

// widgetTarget is where a new widget goes after saving: a board section
// (placed right away) or just the library.
type widgetTarget struct {
	SpaceID, SectionID, BoardID int64
	Version                     int
	Place                       bool
}

func targetOf(get func(string) string) widgetTarget {
	num := func(k string) int64 { n, _ := strconv.ParseInt(get(k), 10, 64); return n }
	version, err := strconv.Atoi(get("version"))
	t := widgetTarget{SpaceID: num("space"), SectionID: num("section"), BoardID: num("board"), Version: version}
	if t.SpaceID == 0 {
		t.SpaceID = num("space_id")
	}
	if t.SectionID == 0 {
		t.SectionID = num("section_id")
	}
	if t.BoardID == 0 {
		t.BoardID = num("board_id")
	}
	t.Place = t.SectionID > 0 && t.BoardID > 0 && err == nil
	return t
}

func (t widgetTarget) Back() string {
	if t.BoardID > 0 {
		return "/boards/" + strconv.FormatInt(t.BoardID, 10) + "?edit"
	}
	return "/widgets"
}

// The editor and the gallery exist only as the board's dialog. Their
// plain address (a link opened in a new tab, the redirect after a copy)
// leads to the page they belong to, which opens them on load (andon.js):
//
//	GET /widgets/7/edit?board=2 ─► /boards/2?edit&editor=/widgets/7/edit?board=2&dialog
const (
	dialogParam = "dialog"
	editorParam = "editor"
)

// toDialog sends a request for the plain editor address to its page;
// false when the request asks for the dialog itself.
func toDialog(w http.ResponseWriter, r *http.Request, target widgetTarget) bool {
	if r.URL.Query().Has(dialogParam) {
		return false
	}
	editor := r.URL.Path + "?" + dialogParam
	if r.URL.RawQuery != "" {
		editor = r.URL.Path + "?" + r.URL.RawQuery + "&" + dialogParam
	}
	back := target.Back()
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+editorParam+"="+url.QueryEscape(editor), http.StatusSeeOther)
	return true
}

// tileLook is how the edited tile sits on its board, so the preview
// shows it that way: the section's tile size, rows and columns.
type tileLook struct {
	Size       enums.TileSize
	Rows, Cols int
}

// lookOf reads the look from the dialog's address (editor.js puts the
// tile's values there); anything unknown means the default.
//
//	?size=small&rows=2 → {small 2 0}
func lookOf(q url.Values) tileLook {
	var look tileLook
	if size := enums.TileSize(q.Get("size")); slices.Contains(tileSizes, size) {
		look.Size = size
	}
	if n, _ := strconv.Atoi(q.Get("rows")); n > 1 && n <= boards.MaxTileRows {
		look.Rows = n
	}
	if n, _ := strconv.Atoi(q.Get("cols")); n > 1 && n <= boards.MaxTileCols {
		look.Cols = n
	}
	return look
}

// tileSizes are the sizes a section can give its tiles.
var tileSizes = []enums.TileSize{enums.TileSmall, enums.TileMedium, enums.TileLarge}

const linkType = "link"

type widgetForm struct {
	Kind    widgets.WidgetType
	Title   string
	Config  map[string]any
	ConnID  *int64
	MinRole string
	Widget  *model.Widget
	Target  widgetTarget
	Look    tileLook
	Error   string
}

func (d Deps) widgetFormPage(w http.ResponseWriter, ctx Ctx, status int, f widgetForm) {
	conns, err := connections.Listing(d.DB, ctx.Who, enums.RightUse)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	var matching []connections.View
	for _, c := range conns {
		if f.Kind.Service == "" || c.Service == f.Kind.Service {
			matching = append(matching, c)
		}
	}
	// A new service tile starts with the caller's first fitting connection.
	if f.Widget == nil && f.ConnID == nil && f.Kind.Service != "" && len(matching) > 0 {
		f.ConnID = &matching[0].ID
	}
	var dest galleryTarget
	if f.Target.Place {
		dest = d.targetNames(ctx, f.Target)
	}

	// The head's fields sit under the title: without one they do nothing.
	var titled, frame []widgets.FormValue
	for _, v := range widgets.FrameFormValues(f.Kind.Key, f.Config) {
		if widgets.NeedsTitle(v.Key) {
			titled = append(titled, v)
			continue
		}
		frame = append(frame, v)
	}
	// A full-width type has no width to pick.
	cols := spanOptions(boards.MaxTileCols)
	if f.Kind.Width == widgets.WidthFull {
		cols = nil
	}
	groups, _ := verbund.Visible(d.DB, ctx.Who)
	// Delete names how many boards lose the tile.
	uses := 0
	if f.Widget != nil {
		uses, _ = widgetlib.Uses(d.DB, f.Widget.ID)
	}
	_ = d.Page(w, ctx, "widget_dialog", status, map[string]any{
		"Uses":    uses,
		"Partial": true, "ThemeURL": "", "Verbuende": groups,
		"Dest": dest, "Look": f.Look, "Topic": widgets.TopicOf(f.Kind.Key), "RowOptions": spanOptions(boards.MaxTileRows), "ColOptions": cols,
		"Kind": f.Kind, "Title": f.Title, "Fields": widgets.FormValues(f.Kind.Key, f.Config),
		"TitleFields": titled, "FrameFields": frame,
		"Conns": matching, "AllConns": conns, "ConnID": f.ConnID, "MinRole": f.MinRole,
		"Widget": f.Widget, "Target": f.Target, "Error": f.Error,
		"NeedsConn":  f.Kind.Service != "" || f.Kind.Category == widgets.CategoryInsight,
		"TeamRoles":  []enums.TeamRole{enums.TeamViewer, enums.TeamEditor, enums.TeamOwner},
		"Categories": []widgets.Category{widgets.CategoryStart, widgets.CategoryInsight},
	})
}

func (d Deps) handleWidgetNewForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	target := targetOf(r.URL.Query().Get)
	if toDialog(w, r, target) {
		return
	}
	spaces := access.EditableSpaces(ctx.Who)
	if target.SpaceID == 0 && len(spaces) > 0 {
		target.SpaceID = spaces[0].ID
	}
	kind, ok := widgets.Get(r.URL.Query().Get("type"))
	if !ok {
		d.handleGallery(w, ctx, target, spaces, galleryAfter{})
		return
	}
	d.widgetFormPage(w, ctx, http.StatusOK, widgetForm{Kind: kind, Target: target})
}

// connectionID parses the widget form's optional "connection_id" field
// ("" means no connection).
func connectionID(r *http.Request) *int64 {
	raw := r.FormValue("connection_id")
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &id
}

func minRole(r *http.Request) *enums.TeamRole {
	raw := r.FormValue("min_role")
	if raw == "" {
		return nil
	}
	role := enums.TeamRole(raw)
	return &role
}

func (d Deps) handleWidgetCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	kind, ok := widgets.Get(r.FormValue("type"))
	if !ok {
		http.Error(w, "widget.unknown_type", http.StatusBadRequest)
		return
	}
	target := targetOf(r.FormValue)
	config := widgets.ParseForm(kind.Key, r.FormValue)
	title := r.FormValue("title")

	id, err := widgetlib.Create(d.DB, ctx.Who, target.SpaceID, kind.Key, title, config, connectionID(r), minRole(r))
	if err != nil {
		d.widgetFormPage(w, ctx, http.StatusBadRequest, widgetForm{Kind: kind, Title: title, Config: config,
			ConnID: connectionID(r), MinRole: r.FormValue("min_role"), Target: target, Error: errKey(err)})
		return
	}
	if target.Place {
		if err := d.placeNew(ctx, target, id, r.FormValue("rows"), r.FormValue("cols")); err != nil {
			d.handleBoardError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, target.Back(), http.StatusSeeOther)
}

// spanOptions lists the spans a new tile can take: 1…most.
//
//	spanOptions(2) → [1 2]
func spanOptions(most int) []int {
	out := make([]int, most)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// placeNew puts a just-created widget into the target section, two rows
// high or two columns wide if asked. Each change bumps the board version.
func (d Deps) placeNew(ctx Ctx, target widgetTarget, widgetID int64, rows, cols string) error {
	placement, err := boards.Place(d.DB, ctx.Who, target.SectionID, widgetID, target.Version)
	if err != nil {
		return err
	}
	version := target.Version + 1

	if n, _ := strconv.Atoi(rows); n > 1 {
		if err := boards.SetTileRows(d.DB, ctx.Who, placement, n, version); err != nil {
			return err
		}
		version++
	}

	if n, _ := strconv.Atoi(cols); n > 1 {
		return boards.SetTileCols(d.DB, ctx.Who, placement, n, version)
	}
	return nil
}

func (d Deps) handleWidgetEditForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	widget, granted, err := widgetlib.Detail(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	if granted < enums.RightEdit {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	kind, _ := widgets.Get(widget.Type)
	role := ""
	if widget.MinTeamRole != nil {
		role = string(*widget.MinTeamRole)
	}
	target := targetOf(r.URL.Query().Get)
	target.SpaceID = widget.SpaceID
	if toDialog(w, r, target) {
		return
	}
	d.widgetFormPage(w, ctx, http.StatusOK, widgetForm{Kind: kind, Title: widget.Title, Config: widget.Config,
		ConnID: widget.ConnectionID, MinRole: role, Widget: widget, Target: target, Look: lookOf(r.URL.Query())})
}

func (d Deps) handleWidgetUpdate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	widget, _, err := widgetlib.Detail(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	kind, _ := widgets.Get(widget.Type)
	config := widgets.ParseForm(kind.Key, r.FormValue)
	version := formInt(r, "widget_version")
	title := r.FormValue("title")
	target := targetOf(r.FormValue)

	if err := widgetlib.Update(d.DB, ctx.Who, id, version, title, config, connectionID(r), minRole(r)); err != nil {
		d.widgetFormPage(w, ctx, http.StatusBadRequest, widgetForm{Kind: kind, Title: title, Config: config,
			ConnID: connectionID(r), MinRole: r.FormValue("min_role"), Widget: widget, Target: target, Error: errKey(err)})
		return
	}
	http.Redirect(w, r, target.Back(), http.StatusSeeOther)
}

// handleWidgetPreview renders the form's current state (htmx, unsaved).
func (d Deps) handleWidgetPreview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	kind, ok := widgets.Get(r.FormValue("type"))
	if !ok {
		http.Error(w, "widget.unknown_type", http.StatusBadRequest)
		return
	}
	space := formID(r, "space_id")
	// As tall as picked for placing: a list shows its extra entries.
	config := widgets.ForRows(kind.Key, widgets.ParseForm(kind.Key, r.FormValue), formInt(r, "rows"))

	// Without a connection a service tile previews demo data.
	conn := connectionID(r)
	if conn == nil && kind.Service != "" {
		frag, err := widgetlib.Demo(r.Context(), d.DB, ctx.Who, space, kind.Key, r.FormValue("title"), config)
		d.renderLivePreview(w, ctx, kind, frag, err)
		return
	}
	frag, err := widgetlib.Preview(r.Context(), d.DB, ctx.Who, space, kind.Key, r.FormValue("title"), config, conn)
	d.renderLivePreview(w, ctx, kind, frag, err)
}

// renderLivePreview writes the editor's preview: link tiles as they are,
// every other tile as a whole card, so its frame and title show too.
func (d Deps) renderLivePreview(w http.ResponseWriter, ctx Ctx, kind widgets.WidgetType, frag *widgetlib.Fragment, err error) {
	if err != nil || kind.Key == linkType {
		d.renderPreview(w, ctx, kind, frag, err)
		return
	}
	_ = d.Page(w, ctx, "preview_card", http.StatusOK, map[string]any{"ThemeURL": "", "Title": frag.Title, "Frame": frag.Frame,
		"Icon": boards.IconOf(frag.Frame.Icon), "Body": &tileBody{Template: kind.Template, Frag: frag}})
}

func (d Deps) handleWidgetDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Each board keeps a revision with a copy of the tile: its undo
	// brings the tile back.
	if _, err := boards.DeleteWidget(d.DB, ctx.Who, id); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, targetOf(r.FormValue).Back(), http.StatusSeeOther)
}
