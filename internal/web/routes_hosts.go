package web

import (
	"errors"
	"net/http"

	"andon/internal/services/hosts"
)

// RegisterHostRoutes wires the host pages: what runs on one machine.
func (d Deps) RegisterHostRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /hosts", d.handleHosts)
	mux.HandleFunc("GET /hosts/{name}", d.handleHost)
}

func (d Deps) handleHosts(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	list, err := hosts.List(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "hosts", http.StatusOK, map[string]any{"Hosts": list})
}

func (d Deps) handleHost(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	host, err := hosts.One(r.Context(), d.DB, ctx.Who, r.PathValue("name"))
	if errors.Is(err, hosts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "host", http.StatusOK, map[string]any{"H": host})
}
