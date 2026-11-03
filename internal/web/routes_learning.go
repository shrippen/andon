package web

import (
	"net/http"
	"strconv"

	"andon/internal/model"
	"andon/internal/services/learn"
)

// The instance's learned models (services/learn):
//
//	GET  /admin/learning          settings and stored models
//	POST /admin/learning          save the settings
//	POST /admin/learning/train    train now
//	POST /admin/learning/reset    forget one model (purpose, conn)
const learningPath = "/admin/learning"

// RegisterLearningRoutes wires the learning page.
func (d Deps) RegisterLearningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+learningPath, d.authed(d.handleLearningPage))
	mux.HandleFunc("POST "+learningPath, d.learningAction(flashSaved, func(r *http.Request, ctx Ctx) error {
		min, _ := strconv.Atoi(r.FormValue("min_percent"))
		hours, _ := strconv.Atoi(r.FormValue("hours"))
		s := learn.Settings{Mode: learn.Mode(r.FormValue("mode")), MinPercent: min, Hours: hours}
		return learn.SaveSettings(d.DB, ctx.Who, s, d.clientIP(r))
	}))
	mux.HandleFunc("POST "+learningPath+"/train", d.learningAction(flashDone, func(r *http.Request, ctx Ctx) error {
		return learn.TrainNow(r.Context(), d.DB, ctx.Who, d.clientIP(r))
	}))
	mux.HandleFunc("POST "+learningPath+"/reset", d.learningAction(flashDone, func(r *http.Request, ctx Ctx) error {
		conn, _ := strconv.ParseInt(r.FormValue("conn"), 10, 64)
		holder, _ := strconv.ParseInt(r.FormValue("holder"), 10, 64)
		return learn.Reset(d.DB, ctx.Who, r.FormValue("purpose"), conn, model.Holder(holder), d.clientIP(r))
	}))
}

func (d Deps) handleLearningPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.learningPage(w, ctx, http.StatusOK, "")
}

// learningPage renders the page; errKey is shown above it.
func (d Deps) learningPage(w http.ResponseWriter, ctx Ctx, status int, errKey string) {
	rows, err := learn.Overview(d.DB, ctx.Who)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s, err := learn.Load(d.DB)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{"Rows": rows, "S": s, "Modes": []learn.Mode{learn.ModeOn, learn.ModeOff}, "Error": errKey}
	_ = d.Page(w, ctx, "admin_learning", status, values)
}

// learningAction runs a post and goes back to the page with flash.
func (d Deps) learningAction(flash string, run func(*http.Request, Ctx) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := d.Require(r)
		if err != nil {
			d.handleAuthError(w, r, err)
			return
		}
		if err := r.ParseForm(); err != nil {
			d.fail(w, err, http.StatusBadRequest)
			return
		}
		if err := run(r, ctx); err != nil {
			d.learningPage(w, ctx, http.StatusBadRequest, errKey(err))
			return
		}
		d.flash(w, flash)
		http.Redirect(w, r, learningPath, http.StatusSeeOther)
	}
}
