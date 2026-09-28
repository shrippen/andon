package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/hints"
)

// RegisterStartPageRoutes wires the start page conveniences: undo, add a
// link by URL, click counting and the command palette's data.
func (d Deps) RegisterStartPageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /boards/{id}/undo", d.authed(d.handleUndo))
	mux.HandleFunc("POST /sections/{id}/quick-link", d.authed(d.handleQuickLink))
	mux.HandleFunc("POST /clicks/{id}", d.handleClick)
	mux.HandleFunc("GET /palette.json", d.authed(d.handlePalette))
	mux.HandleFunc("POST /boards/{id}/duplicate", d.authed(d.handleBoardDuplicate))
	mux.HandleFunc("POST /boards/{id}/bulk", d.authed(d.handleBulk))
}

func (d Deps) handleBoardDuplicate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	copyID, err := boards.Duplicate(d.DB, ctx.Who, id, r.FormValue("name"))
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, boardPath(copyID)+"?edit", http.StatusSeeOther)
}

func (d Deps) handleBulk(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var placements []int64
	for _, raw := range r.Form["placement"] {
		if p, err := strconv.ParseInt(raw, 10, 64); err == nil {
			placements = append(placements, p)
		}
	}
	version, _ := strconv.Atoi(r.FormValue("version"))
	section, _ := strconv.ParseInt(r.FormValue("section"), 10, 64)
	change := boards.BulkChange{Action: boards.BulkAction(r.FormValue("action")), SectionID: section, Color: r.FormValue("color")}
	if err := boards.Bulk(d.DB, ctx.Who, id, version, placements, change); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, boardPath(id)+"?edit&undo", http.StatusSeeOther)
}

// paletteRank orders the palette's groups.
var paletteRank = map[boards.PaletteKind]int{
	boards.PaletteBoard: 0, boards.PaletteLink: 1, boards.PaletteConnection: 2,
	boards.PaletteHints: 3, boards.PalettePage: 4, boards.PaletteSetting: 5,
}

// palettePage is an app page the palette offers, by catalog key.
type palettePage struct {
	key, url string
	kind     boards.PaletteKind
	admin    bool
}

var palettePages = []palettePage{
	{key: "boards.title", url: "/boards", kind: boards.PalettePage}, {key: "nav.hints", url: "/hints", kind: boards.PalettePage},
	{key: "nav.billing", url: "/billing", kind: boards.PalettePage},
	{key: "nav.connections", url: "/connections", kind: boards.PalettePage}, {key: "nav.library", url: "/widgets", kind: boards.PalettePage},
	{key: "nav.timeline", url: "/timeline", kind: boards.PalettePage}, {key: "nav.teams", url: "/teams", kind: boards.PalettePage},
	{key: "nav.import", url: "/import", kind: boards.PalettePage}, {key: "welcome.title", url: "/welcome", kind: boards.PalettePage},
	{key: "nav.space_settings", url: "/spaces/settings", kind: boards.PaletteSetting}, {key: "nav.themes", url: "/themes", kind: boards.PaletteSetting},
	{key: "nav.profile", url: "/me/profile", kind: boards.PaletteSetting}, {key: "nav.security", url: "/me/security", kind: boards.PaletteSetting},
	{key: "nav.credentials", url: "/me/credentials", kind: boards.PaletteSetting}, {key: "nav.notify", url: "/me/notify", kind: boards.PaletteSetting},
	{key: "nav.admin_users", url: "/admin/users", kind: boards.PaletteSetting, admin: true},
	{key: "nav.admin_settings", url: "/admin/settings", kind: boards.PaletteSetting, admin: true},
}

func (d Deps) handleUndo(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := boards.Undo(d.DB, ctx.Who, id); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/boards/"+r.PathValue("id")+"?edit", http.StatusSeeOther)
}

func (d Deps) handleQuickLink(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	section, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	version, _ := strconv.Atoi(r.FormValue("version"))
	if _, err := boards.QuickLink(r.Context(), d.DB, ctx.Who, section, version, r.FormValue("url")); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/boards/"+r.FormValue("board_id")+"?edit&undo", http.StatusSeeOther)
}

// handleClick counts a tile click (sent with navigator.sendBeacon).
func (d Deps) handleClick(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id, err := pathID(r, "id")
	if err == nil {
		_ = boards.Click(d.DB, ctx.Who, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handlePalette(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	items, err := boards.Palette(d.DB, ctx.Who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items = append(items, d.paletteConnections(ctx)...)
	items = append(items, d.paletteHints(ctx)...)
	for _, p := range palettePages {
		if p.admin && !ctx.Who.IsAdmin() {
			continue
		}
		items = append(items, boards.PaletteItem{Kind: p.kind, Title: i18n.T(p.key, ctx.Locale, nil), URL: p.url})
	}
	sort.SliceStable(items, func(i, j int) bool { return paletteRank[items[i].Kind] < paletteRank[items[j].Kind] })
	for i := range items {
		items[i].Group = i18n.T("palette.group_"+string(items[i].Kind), ctx.Locale, nil)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(items)
}

// paletteConnections offers each visible connection, with how it stands;
// editors land on its form.
func (d Deps) paletteConnections(ctx Ctx) []boards.PaletteItem {
	views, err := connections.Listing(d.DB, ctx.Who, enums.RightView)
	if err != nil {
		return nil
	}
	var out []boards.PaletteItem
	for _, v := range views {
		url := "/connections"
		if v.Right >= enums.RightEdit {
			url = fmt.Sprintf("/connections/%d/edit", v.ID)
		}
		detail := i18n.T("conn.state_"+string(v.Health.State()), ctx.Locale, nil)
		if v.Health.FailPct > 0 {
			detail += " · " + i18n.T("health.fail_pct", ctx.Locale, map[string]any{"pct": v.Health.FailPct})
		}
		out = append(out, boards.PaletteItem{Kind: boards.PaletteConnection, Title: v.Name, URL: url, Detail: detail})
	}
	return out
}

// paletteHints offers each rule with open hints, largest group first.
func (d Deps) paletteHints(ctx Ctx) []boards.PaletteItem {
	found, err := hints.Active(d.DB, ctx.Who, 0, nil, 0)
	if err != nil {
		return nil
	}
	var out []boards.PaletteItem
	for _, g := range groupHints(found) {
		out = append(out, boards.PaletteItem{
			Kind: boards.PaletteHints, Title: i18n.T("rule_name."+g.Rule, ctx.Locale, nil), URL: "/hints#rule-" + g.Rule,
			Detail: i18n.T("palette.hint_count", ctx.Locale, map[string]any{"n": g.Count()}),
		})
	}
	return out
}
