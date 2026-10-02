package web

import (
	"net/http"

	"andon/internal/services/about"
)

func (d Deps) RegisterAboutRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /about", d.authed(d.handleAbout))
}

// handleAbout shows what Andon is, which build runs, its database schema, uptime and where its source is.
func (d Deps) handleAbout(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	info, err := about.Get(d.DB)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "about", http.StatusOK, map[string]any{"About": info})
}
