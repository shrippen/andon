package web

import (
	"andon/internal/services/closeticks"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/enums"
	"andon/internal/services/accounts"
	"andon/internal/services/boards"
	"andon/internal/services/hass"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

const compactView = "compact"

// RegisterBoardRoutes wires the home page, board view, widget fragments
// and the personal layout changes (fold, hide, size, order).
func (d Deps) RegisterBoardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", d.authed(d.handleHome))
	mux.HandleFunc("GET /boards/{id}", d.authed(d.handleBoardView))
	mux.HandleFunc("GET /widget-fragments/{id}", d.handleWidgetFragment)
	mux.HandleFunc("GET /details/{id}", d.handleDetail)
	mux.HandleFunc("GET /details/{id}/csv/{n}", d.handleDetailCSV)
	mux.HandleFunc("POST /details/{id}/do/{act}", d.authed(d.handleDetailDo))
	mux.HandleFunc("GET /details/{id}/file", d.authed(d.handleDetailFile))
	mux.HandleFunc("POST /widget-fragments/{id}/toggle", d.authed(d.handleHassToggle))
	mux.HandleFunc("POST /widget-fragments/{id}/kimai", d.authed(d.handleKimaiTimer))
	mux.HandleFunc("POST /widget-fragments/{id}/close", d.authed(d.handleCloseTick))
	mux.HandleFunc("GET /widget-fragments/{id}/kimai/new", d.authed(d.handleKimaiNew))
	mux.HandleFunc("GET /widget-fragments/{id}/kimai/edit", d.authed(d.handleKimaiEdit))
	mux.HandleFunc("GET /widget-fragments/{id}/kimai/day", d.authed(d.handleKimaiDay))
	mux.HandleFunc("POST /widget-fragments/{id}/kimai/pin", d.authed(d.handleKimaiPin))
	mux.HandleFunc("POST /boards/{id}/arrange", d.authed(d.handleArrange))
	mux.HandleFunc("POST /boards/{id}/fold/{sectionID}", d.handleFold)
	mux.HandleFunc("POST /boards/{id}/show/{placementID}", d.handleShow)
	mux.HandleFunc("POST /boards/{id}/rows/{placementID}", d.handleMyRows)
	mux.HandleFunc("POST /boards/{id}/cols/{placementID}", d.handleMyCols)
	mux.HandleFunc("POST /boards/{id}/size/{sectionID}", d.handleSize)
	mux.HandleFunc("POST /boards/{id}/overlay/reset", d.authed(d.handleOverlayReset))
	mux.HandleFunc("GET /boards/{id}/history", d.authed(d.handleHistory))
	mux.HandleFunc("GET /boards/{id}/suggest", d.authed(d.handleSuggest))
	mux.HandleFunc("POST /boards/{id}/suggest", d.authed(d.handleSuggestApply))
	mux.HandleFunc("POST /boards/{id}/restore/{revisionID}", d.handleRestore)
}

func (d Deps) handleHome(w http.ResponseWriter, r *http.Request, ctx Ctx) {

	// The profile's start board wins; StartBoard falls back to the first
	// visible board when it is gone or no longer visible.
	profile, err := accounts.GetProfile(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	id, err := boards.StartBoard(d.DB, ctx.Who, profile.StartBoardID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	target := "/boards/" + strconv.FormatInt(id, 10)
	if r.URL.Query().Has("edit") {
		target += "?edit" // e.g. the welcome checklist: "put a tile on a board"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (d Deps) handleBoardView(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.renderBoard(w, r, ctx, "")
}

// boardMode reads ?edit, ?layout and ?view=compact.
type boardMode struct{ edit, layer, compact bool }

func modeOf(r *http.Request) boardMode {
	q := r.URL.Query()
	return boardMode{edit: q.Has("edit"), layer: q.Has("layout"), compact: q.Get("view") == compactView}
}

// renderBoard shows board {id}. With an embed token the page drops the
// app nav and edit controls, and fragment URLs carry the token along.
func (d Deps) renderBoard(w http.ResponseWriter, r *http.Request, ctx Ctx, embedToken string) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Edit mode shows the shared board, not the editor's own layout.
	want := boards.LayoutOverlay
	if modeOf(r).edit && embedToken == "" {
		want = boards.LayoutBoard
	}
	view, err := boards.View(d.DB, ctx.Who, id, want)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	embed := embedToken != ""
	if embed {
		view.CanEdit = false
	}
	mode := modeOf(r)
	mode.edit = mode.edit && view.CanEdit
	searchEngine := ""
	if !embed {
		if profile, err := accounts.GetProfile(d.DB, ctx.Who); err == nil {
			searchEngine = profile.SearchEngine
		}
	}
	navBoards, err := boards.Nav(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}

	themeURL, err := d.themeURL(ctx.Who, view.ThemeID, &view.Space.ID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}

	var bodies map[int64]*tileBody
	if !embed {
		bodies = d.tileBodies(r, ctx, view)
	}

	_ = d.Page(w, ctx, "board", http.StatusOK, withChoices(map[string]any{
		"Bodies": bodies, "Board": view, "NavBoards": navBoards, "CurBoard": view.ID, "ThemeURL": themeURL,
		"Embed": embed, "EmbedToken": embedToken, "SearchEngine": searchEngine,
		"Edit": mode.edit, "LayerEdit": mode.layer && !embed, "Compact": mode.compact, "UndoHint": r.URL.Query().Has("undo"),
		"Kiosk": kioskOf(r, navBoards, view.ID),
	}))
}

// withChoices adds the section and tile forms' options to a board's data.
func withChoices(data map[string]any) map[string]any {
	data["Sizes"] = []enums.TileSize{enums.TileSmall, enums.TileMedium, enums.TileLarge}
	data["Sorts"] = []enums.SortOrder{enums.SortManual, enums.SortAlphabetical}
	data["Areas"] = []string{"main", "side"}
	data["Spans"] = []int{0, 1, 2, 3, 5, 6, boards.SpanFlow}
	data["RowSpans"] = []int{1, 2, 3, 4}
	data["Colors"] = widgets.TileColors
	data["Mobiles"] = []enums.MobileMode{enums.MobileNormal, enums.MobileFirst, enums.MobileHide}
	return data
}

// ── Section answers ──
//
// An action on one section (tile strip, quick link) sent by htmx gets
// that section back instead of the page: swapping a 240-tile page took
// seconds on a slow device. The board's new version rides along in
// HX-Trigger, and the page carries it into its other forms (editor.js).
//
//	strip form ─POST, HX-Target: dsec-12─► handler ─► boardPart ─► <section id="dsec-12">
//	                                                     └─► HX-Trigger {"boardVersion": 8}

// sectionIDPrefix starts a section element's id: "dsec-12".
const sectionIDPrefix = "dsec-"

// partMode says which tile strips a section answer carries.
type partMode int

const (
	partEdit  partMode = iota // board edit mode
	partLayer                 // the viewer's own layout
)

// partHint says whether the answer points at undo, as ?undo does.
type partHint int

const (
	hintNone partHint = iota
	hintUndo
)

// sectionTarget is the section an htmx request wants back, 0 for the
// whole page.
func sectionTarget(r *http.Request) int64 {
	if r.Header.Get("HX-Request") != "true" {
		return 0
	}
	raw, ok := strings.CutPrefix(r.Header.Get("HX-Target"), sectionIDPrefix)
	if !ok {
		return 0
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

// boardPart answers with the target section of board boardID; false
// means the request wants the whole page. A section that is gone asks
// htmx to reload the page.
func (d Deps) boardPart(w http.ResponseWriter, r *http.Request, ctx Ctx, boardID int64, mode partMode, hint partHint) bool {
	section := sectionTarget(r)
	if section == 0 {
		return false
	}
	want := boards.LayoutOverlay
	if mode == partEdit {
		want = boards.LayoutBoard
	}
	view, err := boards.View(d.DB, ctx.Who, boardID, want)
	if err != nil {
		d.handleBoardError(w, r, err)
		return true
	}

	one := *view
	one.Sections = nil
	for _, sec := range view.Sections {
		if sec.ID == section {
			one.Sections = append(one.Sections, sec)
		}
	}
	if len(one.Sections) == 0 {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return true
	}

	trigger, _ := json.Marshal(map[string]any{"boardVersion": view.Version, "undoHint": hint == hintUndo})
	w.Header().Set("HX-Trigger", string(trigger))
	_ = d.Page(w, ctx, "board_section", http.StatusOK, withChoices(map[string]any{
		"Board": &one, "Section": section, "Bodies": d.tileBodies(r, ctx, &one), "Partial": true, "ThemeURL": "",
		"Edit": mode == partEdit && view.CanEdit, "LayerEdit": mode == partLayer,
	}))
	return true
}

// formBoard is the board a form names in its board_id field.
func formBoard(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.FormValue("board_id"), 10, 64)
	return id
}

// tileBody is a tile's first render, done with the page so tiles don't
// grow one by one as their fragments arrive. Load asks htmx to fetch the
// fragment right away anyway: stored data may be missing or stale.
type tileBody struct {
	PlacementID int64
	Template    string
	Frag        *widgetlib.Fragment
	Load        bool
}

// tileBodies renders every tile from stored data only (svcdata.Stored:
// no request leaves the server), so the page stays as fast as before.
// Link tiles too: one request per status line made a 50-link board send
// 50 requests on every view and on every edit.
func (d Deps) tileBodies(r *http.Request, ctx Ctx, view *boards.BoardView) map[int64]*tileBody {

	// A link without status or info line has no body.
	var ids []int64
	for _, sec := range view.Sections {
		for _, tile := range sec.Tiles {
			kind, ok := widgets.Get(tile.Type)
			if !ok || (tile.Type == linkType && len(kind.Queries(tile.Config)) == 0) {
				continue
			}
			ids = append(ids, tile.PlacementID)
		}
	}
	frags, err := boards.Fragments(r.Context(), d.DB, ctx.Who, view.ID, ids, svcdata.Stored)
	if err != nil {
		return nil
	}

	out := map[int64]*tileBody{}
	for _, sec := range view.Sections {
		for _, tile := range sec.Tiles {
			kind, _ := widgets.Get(tile.Type)
			link := tile.Type == linkType
			frag, ok := frags[tile.PlacementID]
			if !ok {
				continue
			}

			// linkstatus checks every link in the background, so a stored
			// status is as fresh as the tile's own polling would keep it.
			load := needsLoad(kind, tile.Config, frag)
			if link {
				load = pending(frag)
			}
			out[tile.PlacementID] = &tileBody{PlacementID: tile.PlacementID, Template: kind.Template, Frag: frag, Load: load}
		}
	}
	return out
}

// needsLoad: live types want fresh data on every view, sources without a
// connection (weather, feeds) are only fetched on view, once their stored
// value is due, and pending slots have no stored value yet.
func needsLoad(kind widgets.WidgetType, cfg any, frag *widgetlib.Fragment) bool {
	if kind.Live {
		return true
	}
	for _, q := range kind.Queries(cfg) {
		if q.Conn == widgets.ConnNone && frag.Slots[q.Name].Due {
			return true
		}
	}
	return pending(frag)
}

// pending reports a slot without a stored value yet.
func pending(frag *widgetlib.Fragment) bool {
	for _, slot := range frag.Slots {
		if slot.Pending {
			return true
		}
	}
	return false
}

// handleWidgetFragment renders one placed widget's live data (lazy-loaded
// by the board page via htmx), so a slow source never blocks the page.
func (d Deps) handleWidgetFragment(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Viewer(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}

	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	fresh := svcdata.Cached
	if r.URL.Query().Has("refresh") && ctx.CSRF != "" && forceAllowed(id, time.Now()) {
		fresh = svcdata.Force
	}
	d.renderFragment(w, r, ctx, id, fresh)
}

// forceEvery limits ?refresh: a live fetch per tile at most once a
// minute, and only for signed-in viewers (ctx.CSRF set), never through
// an embed token, so an embedding page cannot drain a fetch budget.
const forceEvery = time.Minute

var (
	forcedMu sync.Mutex
	forced   = map[int64]time.Time{}
)

func forceAllowed(placementID int64, now time.Time) bool {
	forcedMu.Lock()
	defer forcedMu.Unlock()

	for id, at := range forced {
		if now.Sub(at) >= forceEvery {
			delete(forced, id)
		}
	}
	if _, recent := forced[placementID]; recent {
		return false
	}
	forced[placementID] = now
	return true
}

func (d Deps) renderFragment(w http.ResponseWriter, r *http.Request, ctx Ctx, placementID int64, fresh svcdata.Freshness) {
	frag, err := boards.Fragment(r.Context(), d.DB, ctx.Who, placementID, fresh)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}

	kind, ok := widgets.Get(frag.Type)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// ThemeURL is irrelevant to a fragment (no <head> here) and would
	// otherwise cost a DB round trip on every htmx refresh.
	_ = d.Page(w, ctx, kind.Template, http.StatusOK, map[string]any{"Frag": frag, "ThemeURL": "", "PlacementID": placementID,
		"Round": frag.Frame.Round})
	if frag.Calm() {
		_, _ = w.Write([]byte(calmMark))
	}
}

// handleHassToggle switches a Home Assistant entity and answers with the
// refreshed tile body (htmx swaps it in place).
func (d Deps) handleHassToggle(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := hass.Toggle(r.Context(), d.DB, ctx.Who, id, r.FormValue("entity"), d.clientIP(r)); err != nil {
		if errors.Is(err, hass.ErrNotSwitchable) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		d.handleBoardError(w, r, err)
		return
	}
	d.renderFragment(w, r, ctx, id, svcdata.Force)
}

// handleCloseTick ticks a month-close step by hand and answers with the
// refreshed tile.
func (d Deps) handleCloseTick(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := closeticks.Toggle(d.DB, ctx.Who, id, r.FormValue("month"), r.FormValue("step")); err != nil {
		if errors.Is(err, closeticks.ErrNotClose) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		d.handleBoardError(w, r, err)
		return
	}
	d.renderFragment(w, r, ctx, id, svcdata.Cached)
}

func (d Deps) handleBoardError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, util.ErrNotFound):
		http.NotFound(w, r)
	default:
		d.fail(w, err, http.StatusInternalServerError)
	}
}

// handleAuthError turns the sentinel errors from Require/Context into the
// right redirect or status code.
func (d Deps) handleAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTOTPPending):
		http.Redirect(w, r, "/login/totp", http.StatusSeeOther)
	case errors.Is(err, ErrTOTPSetup):
		http.Redirect(w, r, "/me/security?totp_required", http.StatusSeeOther)
	case errors.Is(err, ErrLoginRequired):
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case errors.Is(err, ErrCSRFFailed):
		http.Error(w, "csrf", http.StatusForbidden)
	default:
		d.fail(w, err, http.StatusInternalServerError)
	}
}

// ── Personal layout (overlay) and board order ──

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func boardPath(id int64) string { return "/boards/" + strconv.FormatInt(id, 10) }

// arrangeRequest is editor.js's payload: {"version": 3, "layout": {"12": [5, 7]},
// "mode": "board"}; mode is the page's (board in edit mode, else overlay).
type arrangeRequest struct {
	Version int                 `json:"version"`
	Layout  map[string][]int64  `json:"layout"`
	Mode    boards.LayoutTarget `json:"mode"`
}

// handleArrange stores a new tile order: into the board for editors in
// edit mode, else into the caller's own overlay.
func (d Deps) handleArrange(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var body arrangeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpload)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	layout := make(map[int64][]int64, len(body.Layout))
	for key, placements := range body.Layout {
		sectionID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			http.Error(w, "bad section", http.StatusBadRequest)
			return
		}
		layout[sectionID] = placements
	}
	target, version, err := boards.Arrange(d.DB, ctx.Who, id, body.Version, layout, body.Mode)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"target": string(target), "version": version})
}

// layoutAction runs one overlay change and answers with back.
func (d Deps) layoutAction(w http.ResponseWriter, r *http.Request, child string, run func(Ctx, int64, int64) error, back func(int64) string) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err1 := pathID(r, "id")
	childID, err2 := pathID(r, child)
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	if err := run(ctx, id, childID); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	if back != nil && d.boardPart(w, r, ctx, id, partLayer, hintNone) {
		return
	}
	if back == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, back(id), http.StatusSeeOther)
}

func layoutPage(id int64) string { return boardPath(id) + "?layout" }

func (d Deps) handleFold(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "sectionID", func(ctx Ctx, id, sectionID int64) error {
		return boards.FoldSection(d.DB, ctx.Who, id, sectionID, boards.Fold(r.FormValue("state")))
	}, nil)
}

func (d Deps) handleShow(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "placementID", func(ctx Ctx, id, placementID int64) error {
		return boards.ShowTile(d.DB, ctx.Who, id, placementID, boards.Visibility(r.FormValue("state")))
	}, layoutPage)
}

// handleMyRows sets a tile's height in the caller's own layout.
func (d Deps) handleMyRows(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "placementID", func(ctx Ctx, id, placementID int64) error {
		rows := formInt(r, "rows")
		return boards.SetMyTileRows(d.DB, ctx.Who, id, placementID, rows)
	}, layoutPage)
}

// handleMyCols sets a tile's width in the caller's own layout.
func (d Deps) handleMyCols(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "placementID", func(ctx Ctx, id, placementID int64) error {
		cols := formInt(r, "cols")
		return boards.SetMyTileCols(d.DB, ctx.Who, id, placementID, cols)
	}, layoutPage)
}

func (d Deps) handleSize(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "sectionID", func(ctx Ctx, id, sectionID int64) error {
		return boards.ResizeSection(d.DB, ctx.Who, id, sectionID, enums.TileSize(r.FormValue("value")))
	}, layoutPage)
}

func (d Deps) handleOverlayReset(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := boards.ResetOverlay(d.DB, ctx.Who, id); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, boardPath(id), http.StatusSeeOther)
}

// ── History ──

func (d Deps) handleHistory(w http.ResponseWriter, r *http.Request, ctx Ctx) {
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
	revs, err := boards.History(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "board_history", http.StatusOK, map[string]any{"Board": view, "Revisions": revs})
}

func (d Deps) handleRestore(w http.ResponseWriter, r *http.Request) {
	d.layoutAction(w, r, "revisionID", func(ctx Ctx, id, revisionID int64) error {
		return boards.Restore(d.DB, ctx.Who, id, revisionID)
	}, func(id int64) string { return boardPath(id) + "?edit" })
}

// handleSuggest previews a layout built from the space's tiles and
// connections (cold start); nothing changes until it is applied.
func (d Deps) handleSuggest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
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
	suggestion, err := boards.Suggest(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "board_suggest", http.StatusOK, map[string]any{"Board": view, "Suggestion": suggestion})
}

// handleSuggestApply replaces the board's layout with the suggestion.
func (d Deps) handleSuggestApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	version := formInt(r, "version")
	if err := boards.ApplySuggestion(d.DB, ctx.Who, id, version); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/boards/"+strconv.FormatInt(id, 10)+"?edit&undo", http.StatusSeeOther)
}
