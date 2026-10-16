package web

import (
	"net/http"

	"andon/internal/services/release"
)

func (d Deps) RegisterReleaseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /release/hide", d.authed(d.handleReleaseHide))
}

// handleReleaseHide hides the update hint until the next version.
func (d Deps) handleReleaseHide(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := release.Hide(d.DB, ctx.Who, r.FormValue("version")); err != nil {
		d.fail(w, err, http.StatusForbidden)
		return
	}
	http.Redirect(w, r, backTo(r, "/"), http.StatusSeeOther)
}
