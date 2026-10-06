package web

// Verbünde of a space: connections of different services that work
// together (services/verbund). One page per space lists them; forms
// create, rename, change members and delete.

import (
	"net/http"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/services/connections"
	"andon/internal/services/spaces"
	"andon/internal/services/verbund"
)

// RegisterVerbundRoutes wires the Verbünde page and its forms.
func (d Deps) RegisterVerbundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /spaces/{id}/verbund", d.authed(d.handleVerbundPage))
	mux.HandleFunc("POST /verbund", d.authed(d.handleVerbundCreate))
	mux.HandleFunc("POST /verbund/{id}/rename", d.authed(d.handleVerbundRename))
	mux.HandleFunc("POST /verbund/{id}/members", d.authed(d.handleVerbundAdd))
	mux.HandleFunc("POST /verbund/{id}/members/{conn}/remove", d.authed(d.handleVerbundRemove))
	mux.HandleFunc("POST /verbund/{id}/delete", d.authed(d.handleVerbundDelete))
}

// verbundPath is a space's Verbünde page.
func verbundPath(spaceID int64) string { return spacePath(spaceID) + "/verbund" }

func (d Deps) handleVerbundPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if _, known := ctx.Who.Spaces[id]; err != nil || !known || spaces.OpenSettings(d.DB, ctx.Who, id) != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundPage(w, r, ctx, id, http.StatusOK, r.URL.Query().Get("error"))
}

// verbundPage shows the space's Verbünde, the services still ambiguous,
// and the connections a new Verbund may take.
func (d Deps) verbundPage(w http.ResponseWriter, r *http.Request, ctx Ctx, spaceID int64, status int, errKeyText string) {
	list, err := verbund.List(d.DB, ctx.Who, spaceID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	vague, err := verbund.Ambiguous(d.DB, ctx.Who, spaceID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	editable, err := connections.Listing(d.DB, ctx.Who, enums.RightEdit)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	// The space's own connections first, then the others the caller edits.
	ordered := make([]connections.View, 0, len(editable))
	for _, own := range []bool{true, false} {
		for _, c := range editable {
			if (c.SpaceID == spaceID) == own {
				ordered = append(ordered, c)
			}
		}
	}
	values := map[string]any{"Verbuende": list, "Vague": vague, "Editable": ordered, "SpaceID": spaceID, "Error": errKeyText}
	if err := d.levelValues(ctx, spaceID, values); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values[navPath] = verbundPath(spaceID)
	_ = d.Page(w, ctx, "verbund", status, values)
}

// verbundBack returns to the space's page, with an error key if any.
func (d Deps) verbundBack(w http.ResponseWriter, r *http.Request, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	target := verbundPath(space)
	if err != nil {
		target += "?error=" + errKey(err)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (d Deps) handleVerbundCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, raw := range r.Form["conn"] {
		if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	_, err := verbund.Create(d.DB, ctx.Who, r.FormValue("name"), ids, d.clientIP(r))
	d.verbundBack(w, r, err)
}

func (d Deps) handleVerbundRename(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.Rename(d.DB, ctx.Who, id, r.FormValue("name"), d.clientIP(r)))
}

func (d Deps) handleVerbundAdd(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.AddMember(d.DB, ctx.Who, id, formID(r, "conn"), d.clientIP(r)))
}

func (d Deps) handleVerbundRemove(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err1 := pathID(r, "id")
	conn, err2 := pathID(r, "conn")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.RemoveMember(d.DB, ctx.Who, id, conn, d.clientIP(r)))
}

func (d Deps) handleVerbundDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.Delete(d.DB, ctx.Who, id, d.clientIP(r)))
}
