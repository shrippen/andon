package web

import (
	"errors"
	"net/http"

	"andon/internal/services/clients"
)

// RegisterClientRoutes wires the customer pages: every Kimai customer
// with invoices, hours and hints on one card.
func (d Deps) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /clients", d.handleClients)
	mux.HandleFunc("GET /clients/{space}/{id}", d.handleClient)
}

func (d Deps) handleClients(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	cards, err := clients.List(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "clients", http.StatusOK, map[string]any{"Cards": cards})
}

func (d Deps) handleClient(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	spaceID, err1 := pathID(r, "space")
	id, err2 := pathID(r, "id")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	detail, err := clients.One(r.Context(), d.DB, ctx.Who, spaceID, id)
	if errors.Is(err, clients.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "client", http.StatusOK, map[string]any{"C": detail})
}
