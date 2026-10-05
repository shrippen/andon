package web

import (
	"net/http"
	"strconv"

	"andon/internal/enums"
	"andon/internal/services/teams"
	"andon/internal/services/themes"
)

// RegisterTeamRoutes wires /teams: overview, create, rename, member
// set/remove, delete. Admins manage every team; owners manage their own.
func (d Deps) RegisterTeamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /teams", d.authed(d.handleTeamsPage))
	mux.HandleFunc("GET /teams/{id}", d.authed(d.handleTeamPage))
	mux.HandleFunc("POST /teams", d.authed(d.handleTeamCreate))
	mux.HandleFunc("POST /teams/{id}/rename", d.authed(d.handleTeamRename))
	mux.HandleFunc("POST /teams/{id}/members", d.authed(d.handleTeamMemberSet))
	mux.HandleFunc("POST /teams/{id}/members/{userID}/remove", d.authed(d.handleTeamMemberRemove))
	mux.HandleFunc("POST /teams/{id}/delete", d.authed(d.handleTeamDelete))
}

func (d Deps) teamsPage(w http.ResponseWriter, ctx Ctx, status int, extra map[string]any) {
	overview, err := teams.Overview(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	candidates, err := teams.Candidates(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	themeList, err := themes.Listing(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{"Teams": overview, "Users": candidates, "Themes": themeList}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "teams", status, values)
}

func (d Deps) handleTeamsPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.teamsPage(w, ctx, http.StatusOK, nil)
}

// handleTeamPage shows one team (members, its space's settings) as the
// team's first page in the settings frame.
func (d Deps) handleTeamPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := teamID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	overview, err := teams.Overview(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	for _, t := range overview {
		if t.ID == id {
			d.teamsPage(w, ctx, http.StatusOK, map[string]any{"Teams": []teams.View{t}, "Single": true})
			return
		}
	}
	http.NotFound(w, r)
}

func (d Deps) handleTeamCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if _, err := teams.Create(d.DB, ctx.Who, r.FormValue("name"), d.clientIP(r)); err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}

func teamID(r *http.Request) (int64, error) {
	return pathID(r, "id")
}

func (d Deps) handleTeamRename(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := teamID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := teams.Rename(d.DB, ctx.Who, id, r.FormValue("name")); err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}

func (d Deps) handleTeamMemberSet(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := teamID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	userID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	role := enums.TeamRole(r.FormValue("role"))
	if err := teams.SetMember(d.DB, ctx.Who, id, userID, role, d.clientIP(r)); err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}

func (d Deps) handleTeamMemberRemove(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := teamID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	userID, err := pathID(r, "userID")
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := teams.RemoveMember(d.DB, ctx.Who, id, userID, d.clientIP(r)); err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}

func (d Deps) handleTeamDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := teamID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := teams.Delete(d.DB, ctx.Who, id, d.clientIP(r)); err != nil {
		d.teamsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, backTo(r, "/teams"), http.StatusSeeOther)
}
