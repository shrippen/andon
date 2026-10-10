package web

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/enums"
	"andon/internal/services/connections"
)

// movePath is a connection's move page, with the new address filled in
// when known: from the edit form, or a redirect a fetch ran into.
func movePath(id int64, to string) string {
	path := "/connections/" + strconv.FormatInt(id, 10) + "/move"
	if to == "" {
		return path
	}
	return path + "?" + url.Values{"url": {to}}.Encode()
}

// handleMoveForm asks where the connection moves and what its token does.
func (d Deps) handleMoveForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.movePage(w, r, ctx, http.StatusOK, map[string]any{"To": r.URL.Query().Get("url"), "Keep": string(connections.SecretKeep)})
}

// handleMove tests the new address and moves the connection; a failed
// test shows its result and offers to move untested.
func (d Deps) handleMove(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	conn, err := connections.Get(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}

	to, keep := r.FormValue("url"), connections.SecretMove(r.FormValue("keep"))
	values := map[string]any{"To": to, "Keep": string(keep)}
	secret, err := formSecret(r, conn.Service)
	if err != nil {
		values["Error"] = errKey(err)
		d.movePage(w, r, ctx, http.StatusBadRequest, values)
		return
	}
	check := connections.CheckFirst
	if r.FormValue("untested") != "" {
		check = connections.CheckSkip
	}

	result, err := connections.Move(r.Context(), d.DB, ctx.Who, id, to, secret, keep, check)
	if err != nil {
		values["Error"] = errKey(err)
		d.movePage(w, r, ctx, http.StatusBadRequest, values)
		return
	}
	if !result.Ok {
		values["Result"], values["Note"] = result, noteOf(result, conn, ctx.Who)
		d.movePage(w, r, ctx, http.StatusBadRequest, values)
		return
	}
	http.Redirect(w, r, withQuery(recordPath(id, tabOverview), testedFlag, ""), http.StatusSeeOther)
}

// movePage renders the move page for the connection in the path.
func (d Deps) movePage(w http.ResponseWriter, r *http.Request, ctx Ctx, status int, values map[string]any) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	conn, err := connections.Get(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	if conn.Right < enums.RightManage {
		http.NotFound(w, r) // only managers see the settings it belongs to
		return
	}
	values["Conn"] = conn
	_ = d.Page(w, ctx, "connection_move", status, values)
}

// adoptedFlag tells the record it just took over another connection.
const adoptedFlag = "adopted"

// handleAdopt makes the connection take over another one's tiles, links,
// hints and history; the other one is deleted.
func (d Deps) handleAdopt(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := connections.Adopt(d.DB, ctx.Who, id, formID(r, "from")); err != nil {
		d.recordPage(w, r, ctx, tabSettings, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	target := withQuery(withQuery(recordPath(id, tabOverview), testedFlag, ""), adoptedFlag, "")
	http.Redirect(w, r, target, http.StatusSeeOther)
}
