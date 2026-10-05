package web

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/accounts"
	"andon/internal/services/auth"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/hints"
	"andon/internal/services/porting"
	"andon/internal/services/spaces"
	"andon/internal/services/widgetlib"
)

// RegisterMoreRoutes wires the smaller personal and editor actions:
// new board, personal credentials, connection options, ending other
// sessions, team space settings, widget copy and the language switch.
func (d Deps) RegisterMoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /boards", d.authed(d.handleBoardList))
	mux.HandleFunc("POST /boards/{id}/nav", d.authed(d.handleBoardNav))
	mux.HandleFunc("POST /boards/order", d.authed(d.handleBoardOrder))
	mux.HandleFunc("GET /boards/new", d.authed(d.handleBoardNewForm))
	mux.HandleFunc("POST /boards/new", d.authed(d.handleBoardCreate))
	mux.HandleFunc("GET /me/credentials", d.authed(d.handleCredentials))
	mux.HandleFunc("POST /connections/{id}/activate", d.handleActivate)
	mux.HandleFunc("POST /connections/{id}/deactivate", d.handleDeactivate)
	mux.HandleFunc("POST /connections/{id}/options", d.authed(d.handleConnectionOptions))
	mux.HandleFunc("POST /me/security/sessions/others/end", d.authed(d.handleEndOthers))
	mux.HandleFunc("POST /spaces/{id}/team-settings", d.authed(d.handleTeamSpaceSettings))
	mux.HandleFunc("POST /widgets/{id}/copy", d.authed(d.handleWidgetCopy))
	mux.HandleFunc("POST /me/locale", d.authed(d.handleLocale))
}

// handleBoardList shows every board the viewer sees, in their own order,
// to sort and hide in the header and to jump into editing.
func (d Deps) handleBoardList(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := boards.Listed(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	start := int64(0)
	if profile, err := accounts.GetProfile(d.DB, ctx.Who); err == nil && profile.StartBoardID != nil {
		start = *profile.StartBoardID
	} else if len(list) > 0 {
		start = list[0].ID
	}
	cards, err := d.boardCards(ctx, list)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "boards", http.StatusOK, map[string]any{"Boards": cards, "Start": start, "Last": len(list) - 1})
}

// boardCard is a board in the list with its floor plan: tiles by section,
// each tinted by the open hints of its connection.
type boardCard struct {
	boards.BoardRef
	Sections []planSection
	Tiles    int
	Hints    int
}

type planSection struct {
	Cols  int
	Tiles []planTile
}

type planTile struct {
	Cols, Rows int
	Tier       string // Kante tier of its most severe hint, "" = none
}

func (d Deps) boardCards(ctx Ctx, list []boards.BoardRef) ([]boardCard, error) {
	sketches, err := boards.Sketches(d.DB, list)
	if err != nil {
		return nil, err
	}
	badges, err := hints.Badges(d.DB, ctx.Who)
	if err != nil {
		return nil, err
	}

	out := make([]boardCard, 0, len(list))
	for _, ref := range list {
		sketch := sketches[ref.ID]
		card := boardCard{BoardRef: ref, Tiles: sketch.Tiles}
		counted := map[int64]bool{}
		for _, sec := range sketch.Sections {
			part := planSection{Cols: sec.Cols}
			for _, tile := range sec.Tiles {
				plan := planTile{Cols: tile.Cols, Rows: tile.Rows}
				if tile.ConnectionID != nil {
					badge := badges[*tile.ConnectionID]
					if badge.Count > 0 {
						plan.Tier = sevTier(badge.Top)
					}
					// A connection's hints count once, however many tiles show it.
					if !counted[*tile.ConnectionID] {
						counted[*tile.ConnectionID] = true
						card.Hints += badge.Count
					}
				}
				part.Tiles = append(part.Tiles, plan)
			}
			card.Sections = append(card.Sections, part)
		}
		out = append(out, card)
	}
	return out, nil
}

// handleBoardOrder saves the board order after a drag (form field id,
// one per board, in the new order); answers 204, the page has moved it.
func (d Deps) handleBoardOrder(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, raw := range r.Form["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if err := boards.SetNavOrder(d.DB, ctx.Who, ids); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBoardNav moves a board up or down in the viewer's order, or
// shows/hides it in the header.
func (d Deps) handleBoardNav(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch r.FormValue("move") {
	case "up":
		err = boards.MoveNav(d.DB, ctx.Who, id, boards.MoveUp)
	case "down":
		err = boards.MoveNav(d.DB, ctx.Who, id, boards.MoveDown)
	case "toggle":
		err = boards.ToggleNav(d.DB, ctx.Who, id)
	default:
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/boards#board-"+r.PathValue("id"), http.StatusSeeOther)
}

func (d Deps) handleBoardNewForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	_ = d.Page(w, ctx, "board_new", http.StatusOK, map[string]any{"Spaces": access.EditableSpaces(ctx.Who), "Templates": porting.Templates()})
}

func (d Deps) handleBoardCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space, err := strconv.ParseInt(r.FormValue("space_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if tpl := r.FormValue("template"); tpl != "" {
		if _, err := porting.ApplyTemplate(d.DB, ctx.Who, space, tpl); err != nil {
			d.handleBoardError(w, r, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	id, err := boards.Create(d.DB, ctx.Who, space, r.FormValue("name"))
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, boardPath(id)+"?edit", http.StatusSeeOther)
}

// handleCredentials: one's own logins live with the connections now; the
// old address (tile links, bookmarks) leads there.
func (d Deps) handleCredentials(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	mine := access.Personal(ctx.Who)
	if mine == nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, spacePath(mine.ID)+"/connections", http.StatusSeeOther)
}

// activationAction runs a change of a login to a template, for the caller
// or (form field team) a team the caller owns, and returns to the page it
// came from.
func (d Deps) activationAction(w http.ResponseWriter, r *http.Request, run func(Ctx, int64, model.Holder) error) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := model.UserHolder(ctx.Who.UserID)
	if team, err := strconv.ParseInt(r.FormValue("team"), 10, 64); err == nil && team > 0 {
		h = model.TeamHolder(team)
	}
	if err := run(ctx, id, h); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, backTo(r, "/me/credentials"), http.StatusSeeOther)
}

func (d Deps) handleActivate(w http.ResponseWriter, r *http.Request) {
	d.activationAction(w, r, func(ctx Ctx, id int64, h model.Holder) error {
		conn, err := connections.Get(d.DB, ctx.Who, id)
		if err != nil {
			return err
		}
		secret, err := formSecret(r, conn.Service)
		if err != nil {
			return err
		}
		return connections.Activate(d.DB, ctx.Who, id, h, secret)
	})
}

func (d Deps) handleDeactivate(w http.ResponseWriter, r *http.Request) {
	d.activationAction(w, r, func(ctx Ctx, id int64, h model.Holder) error {
		return connections.Deactivate(d.DB, ctx.Who, id, h)
	})
}

// handleConnectionOptions stores a connection's options, edited as YAML.
func (d Deps) handleConnectionOptions(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	options, err := porting.Load(r.FormValue("options"))
	if err == nil {
		err = connections.SetOptions(d.DB, ctx.Who, id, options)
	}
	if err != nil {
		http.Redirect(w, r, withQuery(recordPath(id, tabSettings), "error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, recordPath(id, tabSettings), http.StatusSeeOther)
}

func (d Deps) handleEndOthers(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := auth.EndOtherSessions(d.DB, ctx.Who); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/me/security", http.StatusSeeOther)
}

// handleTeamSpaceSettings stores a team space's hint handling and theme.
func (d Deps) handleTeamSpaceSettings(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mode := enums.AckPerUser
	if enums.HintAckMode(r.FormValue("hint_ack")) == enums.AckTeam {
		mode = enums.AckTeam
	}
	var theme any
	if n, err := strconv.ParseInt(r.FormValue("theme_id"), 10, 64); err == nil {
		theme = float64(n)
	}
	if err := spaces.Update(d.DB, ctx.Who, id, map[string]any{"hint_ack": string(mode), "theme_id": theme}, d.clientIP(r)); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}

func (d Deps) handleWidgetCopy(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err1 := pathID(r, "id")
	space, err2 := strconv.ParseInt(r.FormValue("space_id"), 10, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	newID, err := widgetlib.Copy(d.DB, ctx.Who, id, space)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}

	// From the gallery: place the copy, then adjust it on its edit form.
	edit := "/widgets/" + strconv.FormatInt(newID, 10) + "/edit"
	target := targetOf(r.FormValue)
	if target.Place {
		if err := d.placeNew(ctx, target, newID, "", ""); err != nil {
			d.handleBoardError(w, r, err)
			return
		}
		edit += "?board_id=" + strconv.FormatInt(target.BoardID, 10)
	}
	http.Redirect(w, r, edit, http.StatusSeeOther)
}

// handleLocale switches the language and returns to the page it came from.
func (d Deps) handleLocale(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	locale := enums.Locale(r.FormValue("locale"))
	if err := accounts.UpdateProfile(d.DB, ctx.Who, accounts.ProfileChanges{Locale: &locale}); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	back := "/"
	if ref, err := url.Parse(r.Referer()); err == nil {
		back = safeNext(ref.Path)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
